package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
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

	// The client is built once per backend instance, rather than re-reading
	// the kubeconfig for every step.
	clientOnce sync.Once
	client     kubernetes.Interface
	restCfg    *rest.Config
	clientErr  error

	// Port-forwards opened for service pods, owned by this backend for the
	// executions it runs: forwards maps "<execution>|<node>|<port>/tcp" to the
	// host port, stops holds each execution's stop channels.
	mu       sync.Mutex
	forwards map[string]string
	stops    map[string][]chan struct{}
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

// Capacity is unlimited: the cluster scheduler already holds a pod Pending
// until a node has room, so a second limit here would only queue steps twice.
func (k *Kubernetes) Capacity() int { return 0 }

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

	// The log follower and the pod watcher both emit, so the capture is shared
	// across goroutines.
	var capturedMu sync.Mutex
	capturedLogs := make([]string, 0, 8)
	emitLine := func(entry logstream.Line) {
		if text := strings.TrimSpace(entry.Text); text != "" {
			capturedMu.Lock()
			capturedLogs = append(capturedLogs, text)
			capturedMu.Unlock()
		}
		emit(entry)
	}
	snapshotLogs := func() []string {
		capturedMu.Lock()
		defer capturedMu.Unlock()
		return append([]string(nil), capturedLogs...)
	}

	// Only steps that run an image become pods. Everything else runs exactly
	// as it does on the local backend: a log step is its message; plugin,
	// security and traffic steps are executed by the control plane against the
	// suite's sidecar; a mock service is answered by that sidecar. Turning
	// these into pods is what made most suites fail here for want of an image.
	if step.Node.Kind == "log" {
		emitLogStep(step, emitLine)
		return nil
	}
	if err := requireResolvedStepFile(step); err != nil {
		emitLine(line(step, "error", fmt.Sprintf("[%s] %s", step.Node.Name, err.Error())))
		return err
	}
	if handled, err := runInProcessStep(ctx, step, emitLine, snapshotLogs); handled {
		return err
	}
	if !stepRequiresContainer(step) {
		return runSimulatedStep(ctx, step, emitLine, snapshotLogs)
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

	// A service is started and left running for the steps after it, as the
	// Docker backend does. Waiting for its container to exit, as for a task,
	// held the whole execution until the service was killed.
	if isDetachedService(step) {
		if err := k.startServicePod(ctx, client, step, img, emitLine); err != nil {
			emitLine(line(step, "error", fmt.Sprintf("[%s] Service pod failed: %v", step.Node.Name, err)))
			return err
		}
		return nil
	}

	exitCode, runErr := k.runPod(ctx, client, step, img, emitLine)
	if runErr != nil {
		emitLine(line(step, "error", fmt.Sprintf("[%s] Pod execution failed: %v", step.Node.Name, runErr)))
		return runErr
	}
	return evaluateStepExpectations(step, exitCode, snapshotLogs(), emitLine)
}

func (k *Kubernetes) runPod(ctx context.Context, client kubernetes.Interface, step StepSpec, img string, emit func(logstream.Line)) (int, error) {
	podName := k8sPodName(step)

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

	env := make([]corev1.EnvVar, 0, len(step.Env)+4)
	for key, val := range step.Env {
		env = append(env, corev1.EnvVar{Name: key, Value: val})
	}
	env = append(env, corev1.EnvVar{Name: "BABELSUITE_WORKSPACE_DIR", Value: containerWorkspaceMount})
	// The sidecar address, as the Docker backend exports it — but rewritten
	// for a pod, to which the host-published 127.0.0.1 port means itself.
	if gateway := strings.TrimSpace(step.GatewayURL); gateway != "" {
		env = append(env, corev1.EnvVar{Name: "BABELSUITE_GATEWAY_URL", Value: k8sReachableURL(gateway)})
	}
	if len(step.GatewayURLs) > 0 {
		urls := make([]string, len(step.GatewayURLs))
		for i, u := range step.GatewayURLs {
			urls[i] = k8sReachableURL(u)
		}
		env = append(env, corev1.EnvVar{Name: "BABELSUITE_GATEWAY_URLS", Value: strings.Join(urls, ",")})
	}

	// The same memory ceiling the Docker backend applies: the profile's
	// services.<name>.memory when set, otherwise the default. A fixed 512Mi
	// starved steps the profile had sized up, such as a model server.
	memoryLimit := resource.NewQuantity(int64(containerMemoryLimit), resource.BinarySI)
	if step.Node.MemoryBytes > 0 {
		memoryLimit = resource.NewQuantity(step.Node.MemoryBytes, resource.BinarySI)
	}

	stepContainer := corev1.Container{
		Name:  k8sStepContainer,
		Image: img,
		Env:   env,
		// Left unset, Kubernetes treats an untagged or :latest image —
		// ollama/ollama, apache/apisix:latest — as Always, and every pod goes
		// back to the registry before it can start.
		ImagePullPolicy: corev1.PullIfNotPresent,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse("64Mi"),
				corev1.ResourceCPU:    resource.MustParse("100m"),
			},
			// No CPU limit unless the profile sets services.<name>.cpu, as on
			// Docker. A fixed one-core cap throttled a model server answering
			// on the CPU until the judge plugin timed out.
			Limits: corev1.ResourceList{
				corev1.ResourceMemory: *memoryLimit,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace", MountPath: containerWorkspaceMount},
		},
	}

	if step.Node.MilliCPU > 0 {
		cpu := resource.NewMilliQuantity(step.Node.MilliCPU, resource.DecimalSI)
		stepContainer.Resources.Limits[corev1.ResourceCPU] = *cpu
		// A request above the limit is rejected, and the default request is
		// 100m, so a limit below that pulls the request down with it.
		if step.Node.MilliCPU < stepContainer.Resources.Requests.Cpu().MilliValue() {
			stepContainer.Resources.Requests[corev1.ResourceCPU] = *cpu
		}
	}

	// Run the step's commands or file, as the Docker entrypoint does. Without
	// this a task ran its image's default command and could pass having done
	// nothing at all.
	if script := k8sStepScript(step); script != "" {
		stepContainer.Env = append(stepContainer.Env,
			corev1.EnvVar{Name: "BABELSUITE_SCRIPT", Value: base64.StdEncoding.EncodeToString([]byte(script))})
		stepContainer.Command = []string{"/bin/sh", "-c", "echo $BABELSUITE_SCRIPT | base64 -d | /bin/sh -e"}
	}
	if content := normalizeScriptContent(step.Node.FileContent); content != "" {
		stepContainer.Env = append(stepContainer.Env,
			corev1.EnvVar{Name: "BABELSUITE_FILE", Value: base64.StdEncoding.EncodeToString([]byte(content))})
	}

	// The image's own user, as on Docker. Forcing uid 1000 broke every image
	// that sets up its data directory as root first — postgres cannot chmod
	// its own PGDATA. Tasks and tests drop every capability and cannot
	// escalate, matching the Docker backend; a service keeps the image's
	// defaults there too.
	if !isDetachedService(step) {
		stepContainer.SecurityContext = &corev1.SecurityContext{
			AllowPrivilegeEscalation: &falseVal,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
			SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: k.namespace,
			Labels: map[string]string{
				k8sExecutionLabel: sanitizeID(step.ExecutionID),
				"babelsuite.step": sanitizeID(step.Node.ID),
				"babelsuite.kind": step.Node.Kind,
				// What the Environments page shows the run as.
				labelSuite:   k8sLabelValue(step.SuiteID),
				labelProfile: k8sLabelValue(step.Profile),
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers:    []corev1.Container{stepContainer},
			// The execution's shared workspace, created by SetupExecution: what
			// one step writes to $BABELSUITE_WORKSPACE_DIR the next one reads,
			// as the docs promise and the Docker bind mount provides.
			Volumes: []corev1.Volume{
				{Name: "workspace", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: k8sWorkspaceClaim(step.ExecutionID)},
				}},
			},
			// Every pod of the execution searches its DNS subdomain, so the
			// short names the shipped modules dial — "db:5432",
			// "ollama:11434" — resolve to that execution's service pods, as
			// the per-execution network aliases do on Docker. The kubelet
			// appends it after the namespace's own search domains, so a
			// Service of the same name in the namespace would win; see
			// SetupExecution.
			DNSConfig: &corev1.PodDNSConfig{
				Searches: []string{k.executionDomain(step.ExecutionID)},
			},
		},
	}
	if isDetachedService(step) {
		pod.Spec.Hostname = k8sServiceHostname(step)
		pod.Spec.Subdomain = k8sExecutionSubdomain(step.ExecutionID)
	}

	applyDevicesToPod(pod, step.Node.Devices)

	if withArtifacts {
		sidecarUID := int64(0)
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name:         "artifacts",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
		pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env,
			corev1.EnvVar{Name: "BABELSUITE_ARTIFACTS_DIR", Value: k8sArtifactsMount},
		)
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts,
			corev1.VolumeMount{Name: "artifacts", MountPath: k8sArtifactsMount},
		)
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

func applyDevicesToPod(pod *corev1.Pod, devices []string) {
	if len(devices) == 0 {
		return
	}
	limits := pod.Spec.Containers[0].Resources.Limits
	if limits == nil {
		limits = corev1.ResourceList{}
	}
	for _, d := range devices {
		switch {
		case d == "gpu":
			limits["nvidia.com/gpu"] = resource.MustParse("1")
			pod.Spec.Tolerations = append(pod.Spec.Tolerations, corev1.Toleration{
				Key:      "nvidia.com/gpu",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			})
		case strings.HasPrefix(d, "gpu:"):
			count := strings.TrimPrefix(d, "gpu:")
			limits["nvidia.com/gpu"] = resource.MustParse(count)
			pod.Spec.Tolerations = append(pod.Spec.Tolerations, corev1.Toleration{
				Key:      "nvidia.com/gpu",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			})
		case strings.HasPrefix(d, "/dev/"):
			name := corev1.ResourceName("smarter-devices/" + strings.TrimPrefix(d, "/dev/"))
			limits[name] = resource.MustParse("1")
		}
	}
	pod.Spec.Containers[0].Resources.Limits = limits
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
			if err := podStartFailure(pod); err != nil {
				return 0, err
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
	k.clientOnce.Do(func() {
		k.restCfg, k.clientErr = k.loadRESTConfig()
		if k.clientErr == nil {
			k.client, k.clientErr = kubernetes.NewForConfig(k.restCfg)
		}
	})
	return k.client, k.clientErr
}

func (k *Kubernetes) restConfig() (*rest.Config, error) {
	if _, err := k.newClient(); err != nil {
		return nil, err
	}
	return k.restCfg, nil
}

func (k *Kubernetes) loadRESTConfig() (*rest.Config, error) {
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
