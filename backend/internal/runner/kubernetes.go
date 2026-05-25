package runner

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/strutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	k8sStepContainer   = "step"
	k8sArtifactSidecar = "artifact-collector"
	k8sArtifactsMount  = "/babelsuite-artifacts"
	k8sDeleteTimeout   = 15 * time.Second
	k8sLogDrainTimeout = 5 * time.Second
)

type KubernetesConfig struct {
	BackendConfig
	Namespace  string
	Kubeconfig string
}

type Kubernetes struct {
	config    BackendConfig
	namespace string
	kubecfg   string
}

func NewKubernetes(config KubernetesConfig) *Kubernetes {
	backendConfig := normalizeBackendConfig(config.BackendConfig, "kubernetes", "Kubernetes", "kubernetes")
	return &Kubernetes{
		config:    backendConfig,
		namespace: strutil.FirstNonEmpty(config.Namespace, "default"),
		kubecfg:   config.Kubeconfig,
	}
}

func (k *Kubernetes) ID() string    { return k.config.ID }
func (k *Kubernetes) Label() string { return k.config.Label }
func (k *Kubernetes) Kind() string  { return k.config.Kind }

func (k *Kubernetes) IsAvailable(ctx context.Context) bool {
	client, err := k.newClient()
	if err != nil {
		return false
	}
	ctx2, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err = client.CoreV1().Namespaces().Get(ctx2, k.namespace, metav1.GetOptions{})
	return err == nil
}

func (k *Kubernetes) Run(ctx context.Context, step StepSpec, emit func(logstream.Line)) (err error) {
	spanCtx, span := startStepSpan(ctx, step, "kubernetes")
	defer func() { finishStepSpan(spanCtx, span, step, err) }()
	ctx = spanCtx

	capturedLogs := make([]string, 0, 8)
	emitLine := func(entry logstream.Line) {
		if text := strings.TrimSpace(entry.Text); text != "" {
			capturedLogs = append(capturedLogs, text)
		}
		emit(entry)
	}

	client, err := k.newClient()
	if err != nil {
		return fmt.Errorf("kubernetes client unavailable: %w", err)
	}

	img := step.Node.Image
	if img == "" {
		img = resolveStepImage(step)
	}
	if img == "" {
		return fmt.Errorf("no image configured for step %q", step.Node.Name)
	}

	emitLine(line(step, "info", fmt.Sprintf("[%s] Scheduler reserved an execution slot in namespace %s.", step.Node.Name, k.namespace)))
	emitLine(line(step, "info", bootMessage(step)))

	exitCode, runErr := k.runPod(ctx, client, step, img, emitLine)
	if runErr != nil {
		emitLine(line(step, "error", fmt.Sprintf("[%s] Pod execution failed: %v", step.Node.Name, runErr)))
		return runErr
	}
	return evaluateStepExpectations(step, exitCode, capturedLogs, emitLine)
}

func (k *Kubernetes) runPod(ctx context.Context, client kubernetes.Interface, step StepSpec, img string, emit func(logstream.Line)) (int, error) {
	podName := sanitizeID(fmt.Sprintf("babel-%s-%s", step.ExecutionID, step.Node.ID))
	if len(podName) > 63 {
		podName = podName[:63]
	}
	podName = strings.Trim(podName, "-")

	needsArtifacts := step.OnArtifact != nil && len(step.ArtifactExports) > 0

	pod := k.buildPod(podName, step, img, needsArtifacts)

	emit(line(step, "info", fmt.Sprintf("[%s] Creating Pod %s.", step.Node.Name, podName)))
	if _, err := client.CoreV1().Pods(k.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return 0, fmt.Errorf("pod create: %w", err)
	}
	defer func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), k8sDeleteTimeout)
		defer cancel()
		_ = client.CoreV1().Pods(k.namespace).Delete(rmCtx, podName, metav1.DeleteOptions{})
	}()

	emit(line(step, "info", fmt.Sprintf("[%s] Pod scheduled, waiting for completion.", step.Node.Name)))

	logsDone := make(chan struct{})
	go func() {
		defer close(logsDone)
		k.followLogs(ctx, client, podName, step, emit)
	}()

	exitCode, waitErr := k.waitForStepContainer(ctx, client, podName, emit, step)

	select {
	case <-logsDone:
	case <-time.After(k8sLogDrainTimeout):
	}

	if waitErr != nil {
		return exitCode, waitErr
	}

	if needsArtifacts {
		exitStatus := "success"
		if exitCode != 0 {
			exitStatus = "failure"
		}
		k.collectArtifacts(ctx, client, podName, step, exitStatus)
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Pod finished successfully.", step.Node.Name)))
	return exitCode, nil
}

func (k *Kubernetes) buildPod(podName string, step StepSpec, img string, withArtifacts bool) *corev1.Pod {
	falseVal := false
	trueVal := true
	uid := int64(1000)

	env := make([]corev1.EnvVar, 0, len(step.Env)+1)
	for key, val := range step.Env {
		env = append(env, corev1.EnvVar{Name: key, Value: val})
	}

	stepContainer := corev1.Container{
		Name:  k8sStepContainer,
		Image: img,
		Env:   env,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
				corev1.ResourceCPU:    resource.MustParse("100m"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("512Mi"),
				corev1.ResourceCPU:    resource.MustParse("1000m"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: &falseVal,
			RunAsNonRoot:             &trueVal,
			RunAsUser:                &uid,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: k.namespace,
			Labels: map[string]string{
				"babelsuite.execution": sanitizeID(step.ExecutionID),
				"babelsuite.step":      sanitizeID(step.Node.ID),
				"babelsuite.kind":      step.Node.Kind,
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers:    []corev1.Container{stepContainer},
		},
	}

	if withArtifacts {
		sidecarUID := int64(0)
		pod.Spec.Volumes = []corev1.Volume{
			{
				Name:         "artifacts",
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			},
		}
		pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env,
			corev1.EnvVar{Name: "BABELSUITE_ARTIFACTS_DIR", Value: k8sArtifactsMount},
		)
		pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{Name: "artifacts", MountPath: k8sArtifactsMount},
		}
		// Sidecar keeps the pod alive after the step container exits so we can
		// exec into it to read artifacts before pod deletion.
		pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
			Name:    k8sArtifactSidecar,
			Image:   "busybox:1.36",
			Command: []string{"sh", "-c", "sleep infinity"},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "artifacts", MountPath: k8sArtifactsMount},
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("16Mi"),
					corev1.ResourceCPU:    resource.MustParse("10m"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("32Mi"),
					corev1.ResourceCPU:    resource.MustParse("50m"),
				},
			},
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:                &sidecarUID,
				AllowPrivilegeEscalation: &falseVal,
			},
		})
	}

	return pod
}

// waitForStepContainer watches pod events and returns when the step container terminates.
// It inspects container-level status rather than pod phase so that a running artifact
// sidecar does not prevent detecting step completion.
func (k *Kubernetes) waitForStepContainer(ctx context.Context, client kubernetes.Interface, podName string, emit func(logstream.Line), step StepSpec) (int, error) {
	watcher, err := client.CoreV1().Pods(k.namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", podName),
	})
	if err != nil {
		return 0, fmt.Errorf("pod watch: %w", err)
	}
	defer watcher.Stop()

	runningEmitted := false
	for {
		select {
		case <-ctx.Done():
			return 0, context.Canceled
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return 0, fmt.Errorf("pod watch channel closed unexpectedly")
			}
			if event.Type == watch.Error {
				return 0, fmt.Errorf("pod watch error event received")
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name != k8sStepContainer {
					continue
				}
				if cs.State.Running != nil && !runningEmitted {
					runningEmitted = true
					emit(line(step, "info", fmt.Sprintf("[%s] Pod admitted, sidecars initialized, and readiness probes turned green.", step.Node.Name)))
				}
				t := cs.State.Terminated
				if t == nil {
					continue
				}
				if t.ExitCode != 0 {
					msg := t.Message
					if msg == "" {
						msg = t.Reason
					}
					if msg == "" {
						msg = fmt.Sprintf("exit code %d", t.ExitCode)
					}
					return int(t.ExitCode), fmt.Errorf("step container failed: %s", msg)
				}
				return 0, nil
			}
			// Fallback to pod phase for pods not yet reporting container statuses.
			switch pod.Status.Phase {
			case corev1.PodSucceeded:
				return 0, nil
			case corev1.PodFailed:
				msg := pod.Status.Message
				if msg == "" {
					msg = "pod exited with failure"
				}
				return 1, fmt.Errorf("%s", msg)
			}
		}
	}
}

// followLogs streams the step container's stdout/stderr in real-time.
// It polls until the container is running before opening the log stream.
func (k *Kubernetes) followLogs(ctx context.Context, client kubernetes.Interface, podName string, step StepSpec, emit func(logstream.Line)) {
	if !k.waitForContainerRunning(ctx, client, podName) {
		return
	}
	req := client.CoreV1().Pods(k.namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: k8sStepContainer,
		Follow:    true,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	for scanner.Scan() {
		text := strings.TrimRight(scanner.Text(), "\r\n")
		if text != "" {
			emit(containerLine(step, text))
		}
	}
}

func (k *Kubernetes) waitForContainerRunning(ctx context.Context, client kubernetes.Interface, podName string) bool {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		pod, err := client.CoreV1().Pods(k.namespace).Get(ctx, podName, metav1.GetOptions{})
		if err == nil {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.Name == k8sStepContainer && (cs.State.Running != nil || cs.State.Terminated != nil) {
					return true
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// collectArtifacts reads artifact files from the sidecar container via exec.
// The sidecar shares an emptyDir volume with the step container and stays running
// after the step exits, allowing exec-based reads before pod deletion.
func (k *Kubernetes) collectArtifacts(ctx context.Context, client kubernetes.Interface, podName string, step StepSpec, exitStatus string) {
	cfg, err := k.restConfig()
	if err != nil {
		return
	}
	for _, export := range step.ArtifactExports {
		if !artifactTriggerMatchesStatus(export.On, exitStatus) {
			continue
		}
		content, err := k.execRead(ctx, client, cfg, podName, export.Path)
		if err != nil || len(content) == 0 {
			continue
		}
		step.OnArtifact(export.Path, content)
	}
}

func (k *Kubernetes) execRead(ctx context.Context, client kubernetes.Interface, cfg *rest.Config, podName, filePath string) ([]byte, error) {
	cleanPath := k8sArtifactsMount + "/" + strings.TrimPrefix(strings.TrimSpace(filePath), "/")
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(k.namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: k8sArtifactSidecar,
			Command:   []string{"cat", cleanPath},
			Stdout:    true,
			Stderr:    false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &buf}); err != nil {
		return nil, err
	}

	content := buf.Bytes()
	if len(content) > maxArtifactBytes {
		content = content[:maxArtifactBytes]
	}
	return content, nil
}

func (k *Kubernetes) newClient() (kubernetes.Interface, error) {
	cfg, err := k.restConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}

func (k *Kubernetes) restConfig() (*rest.Config, error) {
	if k.kubecfg != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", k.kubecfg)
		if err != nil {
			return nil, fmt.Errorf("kubernetes config: %w", err)
		}
		return cfg, nil
	}
	cfg, err := rest.InClusterConfig()
	if err != nil {
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			clientcmd.NewDefaultClientConfigLoadingRules(),
			&clientcmd.ConfigOverrides{},
		).ClientConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("kubernetes config: %w", err)
	}
	return cfg, nil
}
