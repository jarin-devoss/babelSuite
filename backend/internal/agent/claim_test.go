package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnOnlineAgentsIdIsNotHandedToAnotherHost(t *testing.T) {
	registry := NewRegistry(nil)
	registry.Register(RegisterRequest{AgentID: "builder", HostURL: "http://host-a:9000", Capacity: 2})

	_, err := registry.Claim(RegisterRequest{AgentID: "builder", HostURL: "http://host-b:9000"})
	if !errors.Is(err, ErrAgentOnline) {
		t.Fatalf("a second host took the id of an online agent: %v", err)
	}
	var held *AgentOnlineError
	if !errors.As(err, &held) || held.HostURL != "http://host-a:9000" || !strings.Contains(err.Error(), "host-a") {
		t.Fatalf("the refusal must say who holds the id: %v", err)
	}
	if record, _ := registry.Get("builder"); record.HostURL != "http://host-a:9000" || record.Capacity != 2 {
		t.Fatalf("the refused registration changed the working agent: %+v", record)
	}
}

func TestTheSameHostMayRegisterAgainAtOnce(t *testing.T) {
	registry := NewRegistry(nil)
	registry.Register(RegisterRequest{AgentID: "builder", HostURL: "http://host-a:9000"})
	if _, err := registry.Claim(RegisterRequest{AgentID: "builder", HostURL: "http://host-a:9100"}); err != nil {
		t.Fatalf("an agent restarted on a new port on the same host was refused: %v", err)
	}
}

func TestForceTakesTheIdOver(t *testing.T) {
	registry := NewRegistry(nil)
	registry.Register(RegisterRequest{AgentID: "builder", HostURL: "http://host-a:9000"})
	record, err := registry.Claim(RegisterRequest{AgentID: "builder", HostURL: "http://host-b:9000", Force: true})
	if err != nil || record.HostURL != "http://host-b:9000" {
		t.Fatalf("force did not take the id over: %v %+v", err, record)
	}
}

func TestAnAgentThatStoppedHeartbeatingCanBeReplaced(t *testing.T) {
	registry := NewRegistry(nil)
	registry.Register(RegisterRequest{AgentID: "builder", HostURL: "http://host-a:9000"})
	registry.mu.Lock()
	stale := registry.agents["builder"]
	stale.LastHeartbeatAt = time.Now().Add(-2 * heartbeatExpiry)
	registry.agents["builder"] = stale
	registry.mu.Unlock()

	if _, err := registry.Claim(RegisterRequest{AgentID: "builder", HostURL: "http://host-b:9000"}); err != nil {
		t.Fatalf("a crashed agent's id was held for ever: %v", err)
	}
}

func TestANewAgentIdIsAccepted(t *testing.T) {
	registry := NewRegistry(nil)
	if _, err := registry.Claim(RegisterRequest{AgentID: "fresh", HostURL: "http://host-a:9000"}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrationOfAHeldIdAnswers409AndTheClientSaysWhy(t *testing.T) {
	registry := NewRegistry(nil)
	registry.Register(RegisterRequest{AgentID: "builder", HostURL: "http://host-a:9000"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request RegisterRequest
		_ = json.NewDecoder(r.Body).Decode(&request)
		if _, err := registry.Claim(request); err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewControlPlaneClient(server.URL, server.Client(), "")
	err := client.Register(context.Background(), RegisterRequest{AgentID: "builder", HostURL: "http://host-b:9000"})
	if !errors.Is(err, ErrAgentIDInUse) || !strings.Contains(err.Error(), "host-a") {
		t.Fatalf("the client must report the id as in use and by whom: %v", err)
	}
	if err := client.Register(context.Background(), RegisterRequest{AgentID: "builder", HostURL: "http://host-b:9000", Force: true}); err != nil {
		t.Fatalf("a forced registration was refused: %v", err)
	}
}
