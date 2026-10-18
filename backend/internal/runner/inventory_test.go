package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/agent"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// The namespace's pods, Service and claim are grouped per execution, with the
// suite and profile the pods are labelled with, and a reap removes them.
func TestKubernetesInventoryGroupsByExecution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := fake.NewClientset()
	k := newTestKubernetes(client)
	if err := k.SetupExecution(ctx, "run-1"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	pod := k.buildPod("babel-run-1-db", StepSpec{
		ExecutionID: "run-1", SuiteID: "payments", Profile: "ci.yaml",
		Node: StepNode{ID: "db", Name: "db", Kind: "service", Image: "postgres:16"},
	}, "postgres:16", false)
	pod.Status.Phase = corev1.PodRunning
	if _, err := client.CoreV1().Pods("ns").Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	runs, err := k.Inventory(ctx)
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}
	if len(runs) != 1 || runs[0].RunID != "run-1" {
		t.Fatalf("runs = %+v, want run-1 only", runs)
	}
	run := runs[0]
	if run.Suite != "payments" || run.Profile != "ci.yaml" {
		t.Errorf("suite/profile = %q/%q", run.Suite, run.Profile)
	}
	if len(run.Containers) != 1 || run.Containers[0].State != "running" || run.Containers[0].Image != "postgres:16" {
		t.Errorf("pods = %+v", run.Containers)
	}
	if len(run.Networks) != 1 || len(run.Volumes) != 1 {
		t.Errorf("service/claims = %d/%d, want 1/1", len(run.Networks), len(run.Volumes))
	}

	if err := k.Reap(ctx, "run-1"); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if runs, _ := k.Inventory(ctx); len(runs) != 0 {
		t.Errorf("after reap: %+v", runs)
	}
}

// The control plane reads a remote agent's inventory over the agent's own API,
// presenting the shared secret, and reaps through it.
func TestRemoteInventoryAsksTheAgent(t *testing.T) {
	t.Parallel()

	var reaped []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			http.Error(w, "agent secret required", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/agent/inventory":
			_ = json.NewEncoder(w).Encode(map[string]any{"executions": []ExecutionInventory{{RunID: "run-9", Containers: []InventoryItem{{ID: "c", Name: "babel-run-9-db", State: "running"}}}}})
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/agent/executions/"):
			reaped = append(reaped, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/agent/executions/"), "/reap"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	remote := NewRemote(RemoteConfig{BackendConfig: BackendConfig{ID: "vm"}, BaseURL: server.URL, Secret: "s3cret", Dispatcher: &countingDispatcher{}})
	runs, err := remote.Inventory(context.Background())
	if err != nil || len(runs) != 1 || runs[0].RunID != "run-9" {
		t.Fatalf("inventory = %+v, %v", runs, err)
	}
	if err := remote.Reap(context.Background(), "run-9"); err != nil || strings.Join(reaped, ",") != "run-9" {
		t.Fatalf("reap: %v, reaped %v", err, reaped)
	}

	wrong := NewRemote(RemoteConfig{BackendConfig: BackendConfig{ID: "vm"}, BaseURL: server.URL, Secret: "nope", Dispatcher: &countingDispatcher{}})
	if _, err := wrong.Inventory(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("wrong secret: err = %v, want a 401", err)
	}
}

// The agent's handler serves the inventory and reap it was configured with.
func TestAgentHandlerServesInventory(t *testing.T) {
	t.Parallel()

	service := agent.NewService(agent.Info{AgentID: "vm"}, nil)
	var reaped string
	service.ConfigureInventory(
		func(context.Context) (any, error) { return []ExecutionInventory{{RunID: "run-3"}}, nil },
		func(_ context.Context, id string) error { reaped = id; return nil },
	)
	server := httptest.NewServer(agent.NewHandler(service, "s3cret"))
	defer server.Close()

	remote := NewRemote(RemoteConfig{BackendConfig: BackendConfig{ID: "vm"}, BaseURL: server.URL, Secret: "s3cret", Dispatcher: &countingDispatcher{}})
	runs, err := remote.Inventory(context.Background())
	if err != nil || len(runs) != 1 || runs[0].RunID != "run-3" {
		t.Fatalf("inventory = %+v, %v", runs, err)
	}
	if err := remote.Reap(context.Background(), "run-3"); err != nil || reaped != "run-3" {
		t.Fatalf("reap: %v, reaped %q", err, reaped)
	}
}
