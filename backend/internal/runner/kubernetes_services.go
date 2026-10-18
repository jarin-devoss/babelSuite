package runner

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

const (
	k8sExecutionLabel = "babelsuite.execution"

	// The cluster DNS domain the execution subdomain is searched under. Every
	// mainstream distribution, Docker Desktop's included, uses this default.
	k8sClusterDomain = "cluster.local"

	// The shared workspace volume's size. ReadWriteOnce, on the cluster's
	// default storage class: a node-local volume binds every pod of the
	// execution to one node, which the scheduler handles on its own.
	k8sWorkspaceSize = "1Gi"

	// How long a service pod may take to reach Running — mostly an image pull
	// on a cold node, which for a model server is minutes, not seconds.
	k8sServiceStartTimeout = 15 * time.Minute
	// The same window the Docker backend gives a detached service to crash
	// before the step is reported started.
	k8sServiceCrashWindow = 500 * time.Millisecond

	// k8sHostAlias is how a pod reaches the machine running the control plane
	// and the suite sidecar. Docker Desktop and kind resolve it inside pods; it
	// is the same name the sidecar itself uses to reach the control plane.
	k8sHostAlias = "host.docker.internal"
)

// Container waiting reasons the kubelet will not recover from by retrying, so
// waiting for Running would only run out the clock. ErrImagePull is left out
// because it precedes ImagePullBackOff while a pull is still being retried.
var k8sFatalWaitingReasons = map[string]bool{
	"ImagePullBackOff":           true,
	"InvalidImageName":           true,
	"CreateContainerConfigError": true,
	"CreateContainerError":       true,
}

// errServiceNotReady is serviceState's "still on its way".
var errServiceNotReady = errors.New("service pod not running yet")

// ── naming ──────────────────────────────────────────────────────────────────

// k8sDNSLabel turns s into an RFC 1123 label: lowercase alphanumerics and
// hyphens, at most 63 characters — what a pod hostname must be. A leading
// digit is kept: a service named "3scale" is dialed as "3scale", and a prefix
// would leave it reachable by no name a step uses. The objects that need the
// stricter RFC 1035 form, a Service's name, are all named "babel-…" and so
// start with a letter anyway. It does not reuse sanitizeID, which cuts at 40
// for container names: a hostname must be the alias other steps dial, and a
// 50-character alias cut to 40 resolves nowhere.
func k8sDNSLabel(s string) string {
	var b strings.Builder
	for _, ch := range strings.ToLower(s) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
		} else {
			b.WriteRune('-')
		}
	}
	s = strings.Trim(b.String(), "-")
	if len(s) > 63 {
		s = strings.TrimRight(s[:63], "-")
	}
	return s
}

// k8sPodName names a step's pod uniquely within the namespace. Cut at 40
// characters, two steps whose ids share their first 40 — a pre-hook service
// "upstream-idp" and its check "upstream-idp-alive" — collided, and the second
// failed with "already exists". A name too long for 63 keeps a hash of the
// full node id instead of its tail.
func k8sPodName(step StepSpec) string {
	full := "babel-" + sanitizeID(step.ExecutionID) + "-" + k8sDNSLabel(step.Node.ID)
	if len(full) <= 63 {
		return strings.TrimRight(full, "-")
	}
	h := fnv.New32a()
	h.Write([]byte(step.Node.ID)) //nolint:errcheck // hash.Hash never errors
	return strings.TrimRight(full[:54], "-") + "-" + fmt.Sprintf("%08x", h.Sum32())
}

// k8sExecutionSubdomain names the headless Service that gives an execution's
// service pods their DNS records.
func k8sExecutionSubdomain(executionID string) string {
	return k8sDNSLabel("babel-" + executionID)
}

// k8sWorkspaceClaim names the execution's shared workspace volume.
func k8sWorkspaceClaim(executionID string) string {
	return k8sDNSLabel("babel-" + executionID + "-workspace")
}

func (k *Kubernetes) executionDomain(executionID string) string {
	return k8sExecutionSubdomain(executionID) + "." + k.namespace + ".svc." + k8sClusterDomain
}

// k8sServiceHostname is the name other steps dial a service by: its first
// network alias, or its node name, exactly as the Docker backend aliases it.
// A pod has one hostname, so only the first alias resolves here.
func k8sServiceHostname(step StepSpec) string {
	alias := step.Node.Name
	if len(step.NetworkAliases) > 0 {
		alias = step.NetworkAliases[0]
	}
	if idx := strings.LastIndex(alias, "/"); idx >= 0 {
		alias = alias[idx+1:]
	}
	if label := k8sDNSLabel(alias); label != "" {
		return label
	}
	return k8sDNSLabel(step.Node.ID)
}

// k8sReachableURL rewrites a loopback URL — the sidecar's host-published port
// — to one a pod can reach. Any other URL is returned unchanged.
func k8sReachableURL(raw string) string {
	return RewriteLoopbackURL(raw, k8sHostAlias)
}

// RewriteLoopbackURL points a loopback URL at host instead, keeping its port:
// the sidecar publishes on the control plane's 127.0.0.1, which a pod or a
// remote agent reaches under another name — host.docker.internal from a pod,
// the control-plane address the agent already dials. Any other URL, or an
// empty host, is returned unchanged.
func RewriteLoopbackURL(raw, host string) string {
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(host) == "" {
		return raw
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		if port := u.Port(); port != "" {
			u.Host = host + ":" + port
		} else {
			u.Host = host
		}
		return u.String()
	}
	return raw
}

// ── step script ─────────────────────────────────────────────────────────────

// normalizeScriptContent converts CRLF and lone CR to LF, as the Docker
// backend does before writing a step file, so scripts authored on Windows run
// under a Linux shell.
func normalizeScriptContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.ReplaceAll(content, "\r", "\n")
}

// k8sStepScript is buildStepScript with the step file written first. Docker
// writes that file into the workspace from the host before the container
// starts; a pod is started by the cluster, so the file travels in
// BABELSUITE_FILE and the script puts it where buildStepScript expects it.
func k8sStepScript(step StepSpec) string {
	script := buildStepScript(step)
	if script == "" || step.Node.FileContent == "" {
		return script
	}
	target := containerWorkspaceMount + "/" + step.Node.File
	prelude := "mkdir -p '" + path.Dir(target) + "'\n" +
		"printf '%s' \"$BABELSUITE_FILE\" | base64 -d > '" + target + "'\n"
	return prelude + script
}

// ── execution lifecycle ─────────────────────────────────────────────────────

// SetupExecution creates what every pod of the execution shares: the headless
// Service that gives service pods their DNS records, and the workspace
// volume.
//
// The Service has no ports. It exists so that each service pod, with hostname
// set and this Service as its subdomain, gets an A record at
// <hostname>.<subdomain>.<namespace>.svc. Every pod searches that domain, so
// a short name resolves within its own execution, and two executions that
// both start a "db" do not collide in a shared namespace. The kubelet appends
// the search domain after the ClusterFirst ones, though, so
// <name>.<namespace>.svc is tried first: a Service named "db" in the
// namespace would shadow every execution's "db". The namespace is meant to
// hold BabelSuite runs only, whose Services are all named babel-…. Not-ready
// addresses are published because a service is reported started once Running,
// before any readiness probe the image defines has passed.
func (k *Kubernetes) SetupExecution(ctx context.Context, executionID string) error {
	client, err := k.newClient()
	if err != nil {
		return fmt.Errorf("kubernetes client unavailable: %w", err)
	}
	labels := map[string]string{k8sExecutionLabel: sanitizeID(executionID)}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: k8sExecutionSubdomain(executionID), Namespace: k.namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 labels,
			PublishNotReadyAddresses: true,
		},
	}
	if _, err := client.CoreV1().Services(k.namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create headless service: %w", err)
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: k8sWorkspaceClaim(executionID), Namespace: k.namespace, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(k8sWorkspaceSize)},
			},
		},
	}
	if _, err := client.CoreV1().PersistentVolumeClaims(k.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create workspace volume: %w", err)
	}
	return nil
}

// DestroyExecution removes everything the execution left in the cluster, by
// name and label rather than from memory, so it is safe to call when setup
// failed or never ran. Port-forwards close first, before the pods they point
// at go.
func (k *Kubernetes) DestroyExecution(ctx context.Context, executionID string) error {
	k.closeForwards(executionID)

	client, err := k.newClient()
	if err != nil {
		return err
	}
	var errs []error
	// One delete per pod by name, rather than DeleteCollection: the
	// documented service-account permissions are create/get/watch/delete on
	// pods, and a collection delete needs a verb of its own.
	pods, err := client.CoreV1().Pods(k.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: k8sExecutionLabel + "=" + sanitizeID(executionID),
	})
	if err != nil {
		errs = append(errs, err)
	} else {
		for _, pod := range pods.Items {
			if err := client.CoreV1().Pods(k.namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
				errs = append(errs, err)
			}
		}
	}
	if err := client.CoreV1().Services(k.namespace).Delete(ctx, k8sExecutionSubdomain(executionID), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, err)
	}
	// The pvc-protection finalizer holds the volume until its pods are gone.
	if err := client.CoreV1().PersistentVolumeClaims(k.namespace).Delete(ctx, k8sWorkspaceClaim(executionID), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// ── service pods ────────────────────────────────────────────────────────────

// startServicePod starts a service and returns once it is running, leaving it
// up for the steps after it — the Docker backend's detached service. The pod
// is removed by DestroyExecution, or here if it never comes up.
func (k *Kubernetes) startServicePod(ctx context.Context, client kubernetes.Interface, step StepSpec, img string, emit func(logstream.Line)) error {
	podName := k8sPodName(step)
	pod := k.buildPod(podName, step, img, false)

	emit(line(step, "info", fmt.Sprintf("[%s] Creating service Pod %s, reachable as %s.", step.Node.Name, podName, pod.Spec.Hostname)))
	if _, err := client.CoreV1().Pods(k.namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("pod create: %w", err)
	}
	removePod := func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), k8sDeleteTimeout)
		defer cancel()
		_ = client.CoreV1().Pods(k.namespace).Delete(rmCtx, podName, metav1.DeleteOptions{})
	}

	if err := k.waitForServiceRunning(ctx, client, podName); err != nil {
		removePod()
		return err
	}
	if err := k.forwardServicePorts(client, step, podName); err != nil {
		removePod()
		return err
	}

	// The pod outlives this step, so its logs are followed on a context that
	// does too; the stream ends when the pod is deleted.
	go k.followLogs(context.Background(), client, podName, step, emit)
	emit(line(step, "info", fmt.Sprintf("[%s] Service Pod is running.", step.Node.Name)))
	return nil
}

// waitForServiceRunning watches the pod until its container runs, then gives
// it the crash window. It is driven by pod events rather than polling, and
// fails fast on states the kubelet will not recover from instead of waiting
// out the timeout.
func (k *Kubernetes) waitForServiceRunning(ctx context.Context, client kubernetes.Interface, podName string) error {
	ctx, cancel := context.WithTimeout(ctx, k8sServiceStartTimeout)
	defer cancel()

	watcher, err := client.CoreV1().Pods(k.namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: "metadata.name=" + podName,
	})
	if err != nil {
		return fmt.Errorf("pod watch: %w", err)
	}
	defer watcher.Stop()

	for running := false; !running; {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("pod %s did not reach Running within %s", podName, k8sServiceStartTimeout)
			}
			return context.Canceled
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("pod watch closed before %s was running", podName)
			}
			if event.Type == watch.Error {
				return fmt.Errorf("pod watch error for %s", podName)
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			switch err := serviceState(pod); {
			case err == nil:
				running = true
			case !errors.Is(err, errServiceNotReady):
				return err
			}
		}
	}

	select {
	case <-ctx.Done():
		return context.Canceled
	case <-time.After(k8sServiceCrashWindow):
	}
	pod, err := client.CoreV1().Pods(k.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get pod %s: %w", podName, err)
	}
	return serviceState(pod)
}

// podStartFailure reports a pod that will never start its step container: one
// no node can take — a GPU or device the cluster does not have — or an image
// the kubelet has given up pulling. It fails on exactly these rather than
// waiting for the clock to run out; without it a task asking for a GPU sat at
// "Pod scheduled, waiting for completion" for as long as the execution
// allowed.
func podStartFailure(pod *corev1.Pod) error {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == corev1.PodReasonUnschedulable {
			return fmt.Errorf("pod %s is unschedulable: %s", pod.Name, cond.Message)
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != k8sStepContainer {
			continue
		}
		if w := cs.State.Waiting; w != nil && k8sFatalWaitingReasons[w.Reason] {
			return fmt.Errorf("container cannot start (%s): %s", w.Reason, w.Message)
		}
	}
	return nil
}

// serviceState returns nil once the step container runs, errServiceNotReady
// while it is still on its way, and a descriptive error for any state it
// cannot recover from.
func serviceState(pod *corev1.Pod) error {
	if err := podStartFailure(pod); err != nil {
		return err
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != k8sStepContainer {
			continue
		}
		if t := cs.State.Terminated; t != nil {
			reason := t.Reason
			if t.Message != "" {
				reason += ": " + t.Message
			}
			return fmt.Errorf("service exited immediately with code %d (%s)", t.ExitCode, reason)
		}
		if cs.State.Running != nil {
			return nil
		}
	}
	switch pod.Status.Phase {
	case corev1.PodFailed, corev1.PodSucceeded:
		return fmt.Errorf("service pod %s ended in phase %s: %s", pod.Name, pod.Status.Phase, pod.Status.Message)
	}
	return errServiceNotReady
}
