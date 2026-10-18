package runner

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/logstream"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// Steps with no image must not need a cluster at all: they ran as pods before
// and every suite with a mock or a log step failed with "no image configured".
// The kubeconfig points nowhere, so reaching for a client fails the test.
func TestKubernetesRunsImagelessStepsWithoutAPod(t *testing.T) {
	t.Parallel()

	k := NewKubernetes(KubernetesConfig{Namespace: "ns", Kubeconfig: "does-not-exist"})
	for _, node := range []StepNode{
		{ID: "say", Name: "say", Kind: "log", Variant: "log.info", Message: "hello"},
		{ID: "agent-model", Name: "agent-model", Kind: "mock"},
		{ID: "noop", Name: "noop", Kind: "task"},
	} {
		var lines []logstream.Line
		err := k.Run(context.Background(), StepSpec{ExecutionID: "run-1", Node: node}, func(l logstream.Line) { lines = append(lines, l) })
		if err != nil {
			t.Errorf("%s step: %v", node.Kind, err)
		}
		if len(lines) == 0 {
			t.Errorf("%s step emitted nothing", node.Kind)
		}
	}
}

func TestKubernetesBuildPodMatchesDocker(t *testing.T) {
	t.Parallel()

	k := NewKubernetes(KubernetesConfig{Namespace: "babelsuite-runs"})
	task := StepSpec{ExecutionID: "run-abc", Node: StepNode{
		ID: "migrate", Name: "migrate", Kind: "task", Image: "alpine:3.19",
		Commands: []string{"echo migrating"},
	}}
	pod := k.buildPod("babel-run-abc-migrate", task, "alpine:3.19", false)
	c := pod.Spec.Containers[0]

	if sc := c.SecurityContext; sc == nil || sc.RunAsUser != nil || sc.RunAsNonRoot != nil {
		t.Errorf("task pod must keep the image's user and only drop capabilities, got %+v", sc)
	} else if len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" || *sc.AllowPrivilegeEscalation {
		t.Errorf("task pod must drop ALL and forbid escalation, got %+v", sc)
	}
	if len(c.Command) == 0 || !strings.Contains(strings.Join(c.Command, " "), "BABELSUITE_SCRIPT") {
		t.Fatalf("task pod must run its script, got command %v", c.Command)
	}
	script := envValue(c.Env, "BABELSUITE_SCRIPT")
	decoded, _ := base64.StdEncoding.DecodeString(script)
	if !strings.Contains(string(decoded), "echo migrating") {
		t.Errorf("script does not carry the step's commands: %q", decoded)
	}
	if c.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("pull policy = %q, want IfNotPresent so untagged images are not re-pulled per pod", c.ImagePullPolicy)
	}
	if _, capped := c.Resources.Limits[corev1.ResourceCPU]; capped {
		t.Error("pods must have no CPU limit, matching the Docker backend")
	}
	if got := c.Resources.Limits.Memory().Value(); got != int64(containerMemoryLimit) {
		t.Errorf("default memory limit = %d, want %d", got, containerMemoryLimit)
	}
	if pod.Spec.Hostname != "" {
		t.Errorf("a task needs no hostname, got %q", pod.Spec.Hostname)
	}
	if v := pod.Spec.Volumes[0]; v.PersistentVolumeClaim == nil || v.PersistentVolumeClaim.ClaimName != "babel-run-abc-workspace" {
		t.Errorf("workspace must be the execution's shared volume, got %+v", v.VolumeSource)
	}
	if want := "babel-run-abc.babelsuite-runs.svc.cluster.local"; len(pod.Spec.DNSConfig.Searches) != 1 || pod.Spec.DNSConfig.Searches[0] != want {
		t.Errorf("DNS searches = %v, want [%s]", pod.Spec.DNSConfig.Searches, want)
	}

	svc := StepSpec{ExecutionID: "run-abc", NetworkAliases: []string{"db"}, Node: StepNode{
		ID: "returns-db", Name: "returns/db", Kind: "service", Image: "postgres:16", MemoryBytes: 2 << 30,
	}}
	pod = k.buildPod("babel-run-abc-returns-db", svc, "postgres:16", false)
	if pod.Spec.Containers[0].SecurityContext != nil {
		t.Errorf("a service keeps the image's security defaults, as on Docker; got %+v", pod.Spec.Containers[0].SecurityContext)
	}
	if pod.Spec.Hostname != "db" || pod.Spec.Subdomain != "babel-run-abc" {
		t.Errorf("service DNS = %s.%s, want db.babel-run-abc", pod.Spec.Hostname, pod.Spec.Subdomain)
	}
	if got := pod.Spec.Containers[0].Resources.Limits.Memory().Value(); got != 2<<30 {
		t.Errorf("profile memory limit = %d, want %d", got, 2<<30)
	}

	// services.<name>.cpu becomes limits.cpu; a limit under the default
	// 100m request pulls the request down so the pod stays valid.
	svc.Node.MilliCPU = 1500
	pod = k.buildPod("p", svc, "postgres:16", false)
	if got := pod.Spec.Containers[0].Resources.Limits.Cpu().MilliValue(); got != 1500 {
		t.Errorf("cpu limit = %dm, want 1500m", got)
	}
	svc.Node.MilliCPU = 50
	pod = k.buildPod("p", svc, "postgres:16", false)
	res := pod.Spec.Containers[0].Resources
	if res.Limits.Cpu().MilliValue() != 50 || res.Requests.Cpu().MilliValue() > 50 {
		t.Errorf("cpu limit 50m: limits=%v requests=%v", res.Limits.Cpu(), res.Requests.Cpu())
	}
}

func TestK8sStepScriptWritesTheStepFileFirst(t *testing.T) {
	t.Parallel()

	step := StepSpec{Node: StepNode{Kind: "task", File: "tasks/seed.sh", FileContent: "echo seeded\r\n"}}
	script := k8sStepScript(step)
	write := strings.Index(script, "base64 -d > '"+containerWorkspaceMount+"/tasks/seed.sh'")
	run := strings.Index(script, "sh "+containerWorkspaceMount+"/tasks/seed.sh")
	if write < 0 || run < 0 || write > run {
		t.Fatalf("the file must be written before it is run:\n%s", script)
	}
	if got := normalizeScriptContent(step.Node.FileContent); got != "echo seeded\n" {
		t.Errorf("CRLF not normalized: %q", got)
	}
}

func TestK8sReachableURL(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"http://127.0.0.1:51536":      "http://host.docker.internal:51536",
		"http://localhost:9080/mocks": "http://host.docker.internal:9080/mocks",
		"http://agent-model:9080":     "http://agent-model:9080",
	} {
		if got := k8sReachableURL(in); got != want {
			t.Errorf("k8sReachableURL(%q) = %q, want %q", in, got, want)
		}
	}

	k := NewKubernetes(KubernetesConfig{Namespace: "ns"})
	pod := k.buildPod("p", StepSpec{ExecutionID: "run-1", GatewayURL: "http://127.0.0.1:51536",
		Node: StepNode{ID: "t", Name: "t", Kind: "test", Image: "curl", Commands: []string{"true"}}}, "curl", false)
	if got := envValue(pod.Spec.Containers[0].Env, "BABELSUITE_GATEWAY_URL"); got != "http://host.docker.internal:51536" {
		t.Errorf("pod BABELSUITE_GATEWAY_URL = %q", got)
	}
}

// A service's pod hostname is the name steps dial, digits first included.
func TestK8sServiceHostnameKeepsTheDialedName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		step StepSpec
		want string
	}{
		{StepSpec{Node: StepNode{ID: "3scale", Name: "3scale", Kind: "service"}}, "3scale"},
		{StepSpec{Node: StepNode{ID: "identity-broker/db", Name: "identity-broker/db", Kind: "service"}}, "db"},
		{StepSpec{NetworkAliases: []string{"identity-broker-db"}, Node: StepNode{ID: "identity-broker/db", Name: "identity-broker/db", Kind: "service"}}, "identity-broker-db"},
	} {
		if got := k8sServiceHostname(tc.step); got != tc.want {
			t.Errorf("k8sServiceHostname(%s) = %q, want %q", tc.step.Node.ID, got, tc.want)
		}
	}
}

func TestK8sDNSLabel(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"db":                    "db",
		"Returns_DB":            "returns-db",
		"123-api":               "123-api",
		"--edge--":              "edge",
		strings.Repeat("a", 80): strings.Repeat("a", 63),
	} {
		if got := k8sDNSLabel(in); got != want {
			t.Errorf("k8sDNSLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// The identity-broker pre-hook has a service "upstream-idp" and a check
// "upstream-idp-alive"; cut at 40 characters both names were the same pod.
func TestK8sPodNamesStayUniqueWhenLong(t *testing.T) {
	t.Parallel()

	a := k8sPodName(StepSpec{ExecutionID: "run-6b7656be", Node: StepNode{ID: "pre-hook/upstream-idp"}})
	b := k8sPodName(StepSpec{ExecutionID: "run-6b7656be", Node: StepNode{ID: "pre-hook/upstream-idp-alive"}})
	if a == b {
		t.Fatalf("both steps got pod name %q", a)
	}
	longA := k8sPodName(StepSpec{ExecutionID: "run-6b7656be", Node: StepNode{ID: "identity-broker/pre-hook/upstream-idp"}})
	longB := k8sPodName(StepSpec{ExecutionID: "run-6b7656be", Node: StepNode{ID: "identity-broker/pre-hook/upstream-idp-alive"}})
	if longA == longB {
		t.Fatalf("both long steps got pod name %q", longA)
	}
	for _, n := range []string{a, b, longA, longB} {
		if len(n) > 63 || n != k8sDNSLabel(n) {
			t.Errorf("pod name %q is not a valid DNS label", n)
		}
	}
}

// newTestKubernetes returns a backend whose client is the given fake, as if
// newClient had already built it.
func newTestKubernetes(client kubernetes.Interface) *Kubernetes {
	k := NewKubernetes(KubernetesConfig{Namespace: "ns"})
	k.clientOnce.Do(func() { k.client = client })
	return k
}

func TestSetupAndDestroyExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stray := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "babel-run-life-db", Namespace: "ns",
		Labels: map[string]string{k8sExecutionLabel: "run-life"}}}
	client := fake.NewSimpleClientset(stray)
	k := newTestKubernetes(client)

	// Destroy before setup must be harmless.
	if err := k.DestroyExecution(ctx, "run-never"); err != nil {
		t.Errorf("DestroyExecution without setup: %v", err)
	}

	if err := k.SetupExecution(ctx, "run-life"); err != nil {
		t.Fatalf("SetupExecution: %v", err)
	}
	if err := k.SetupExecution(ctx, "run-life"); err != nil {
		t.Fatalf("SetupExecution must be idempotent: %v", err)
	}
	svc, err := client.CoreV1().Services("ns").Get(ctx, "babel-run-life", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("headless service missing: %v", err)
	}
	if svc.Spec.ClusterIP != corev1.ClusterIPNone || !svc.Spec.PublishNotReadyAddresses || svc.Spec.Selector[k8sExecutionLabel] != "run-life" {
		t.Errorf("service must be headless, publish not-ready addresses and select the execution: %+v", svc.Spec)
	}
	pvc, err := client.CoreV1().PersistentVolumeClaims("ns").Get(ctx, "babel-run-life-workspace", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("workspace volume missing: %v", err)
	}
	if pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("workspace access mode = %v", pvc.Spec.AccessModes)
	}

	stop := make(chan struct{})
	k.registerForward("run-life", k8sForwardKey("run-life", "broker", "8082/tcp"), "54321", stop)

	if err := k.DestroyExecution(ctx, "run-life"); err != nil {
		t.Fatalf("DestroyExecution: %v", err)
	}
	select {
	case <-stop:
	default:
		t.Error("DestroyExecution left a port-forward open")
	}
	if _, err := client.CoreV1().Services("ns").Get(ctx, "babel-run-life", metav1.GetOptions{}); err == nil {
		t.Error("headless service left behind")
	}
	if _, err := client.CoreV1().PersistentVolumeClaims("ns").Get(ctx, "babel-run-life-workspace", metav1.GetOptions{}); err == nil {
		t.Error("workspace volume left behind")
	}
	if pods, _ := client.CoreV1().Pods("ns").List(ctx, metav1.ListOptions{}); len(pods.Items) != 0 {
		t.Errorf("execution pods left behind: %d", len(pods.Items))
	}
	if err := k.DestroyExecution(ctx, "run-life"); err != nil {
		t.Errorf("second DestroyExecution: %v", err)
	}
}

func TestServiceStateFailsFastOnUnrecoverablePods(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		status  corev1.PodStatus
		wantErr error  // errServiceNotReady, or nil for running
		errPart string // set for unrecoverable states
	}{
		"running": {status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: k8sStepContainer, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}},
		"pulling": {status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: k8sStepContainer, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull"}}}}}, wantErr: errServiceNotReady},
		"backoff": {status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: k8sStepContainer, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "not found"}}}}}, errPart: "ImagePullBackOff"},
		"crashed": {status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
			{Name: k8sStepContainer, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"}}}}}, errPart: "exited immediately with code 1"},
		"unschedulable": {status: corev1.PodStatus{Conditions: []corev1.PodCondition{
			{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: "Insufficient memory"}}}, errPart: "unschedulable"},
	}
	for name, tc := range cases {
		err := serviceState(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p"}, Status: tc.status})
		switch {
		case tc.errPart != "":
			if err == nil || errors.Is(err, errServiceNotReady) || !strings.Contains(err.Error(), tc.errPart) {
				t.Errorf("%s: err = %v, want an unrecoverable error mentioning %q", name, err, tc.errPart)
			}
		case !errors.Is(err, tc.wantErr) && err != tc.wantErr:
			t.Errorf("%s: err = %v, want %v", name, err, tc.wantErr)
		}
	}
}

// A task asking for a GPU the cluster lacks must fail at once, not sit at
// "Pod scheduled, waiting for completion" — returns-control-plane/peak.yaml.
func TestPodStartFailureCoversTaskPods(t *testing.T) {
	t.Parallel()

	pending := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "seed-policies"}, Status: corev1.PodStatus{
		Phase: corev1.PodPending,
		Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse,
			Reason: corev1.PodReasonUnschedulable, Message: "0/3 nodes are available: 2 Insufficient nvidia.com/gpu."}},
	}}
	if err := podStartFailure(pending); err == nil || !strings.Contains(err.Error(), "nvidia.com/gpu") {
		t.Errorf("podStartFailure = %v, want the scheduler's reason", err)
	}
	creating := &corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{Name: k8sStepContainer, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}}}}}
	if err := podStartFailure(creating); err != nil {
		t.Errorf("a pod still creating is not a failure: %v", err)
	}
}

// The runtime asks the step's backend for a sibling service's host port; on
// Kubernetes that is the port-forward its service step opened.
func TestKubernetesHostPortServesForwards(t *testing.T) {
	t.Parallel()

	k := newTestKubernetes(fake.NewSimpleClientset())
	var resolver HostPortResolver = k
	k.registerForward("run-fwd", k8sForwardKey("run-fwd", "broker", "8082/tcp"), "54321", make(chan struct{}))

	if got := resolver.HostPort("run-fwd", "broker", "8082/tcp"); got != "54321" {
		t.Errorf("HostPort = %q, want the forwarded 54321", got)
	}
	if got := resolver.HostPort("run-fwd", "broker", "9092/tcp"); got != "" {
		t.Errorf("an unforwarded port answered %q", got)
	}
	k.closeForwards("run-fwd")
	if got := resolver.HostPort("run-fwd", "broker", "8082/tcp"); got != "" {
		t.Errorf("forward still registered after close: %q", got)
	}
}

func envValue(env []corev1.EnvVar, name string) string {
	for _, e := range env {
		if e.Name == name {
			return e.Value
		}
	}
	return ""
}
