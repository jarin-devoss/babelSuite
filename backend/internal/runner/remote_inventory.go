package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// remoteInventoryClient is the control plane's side of the agent's inventory
// API: the agent's containers run on its own Docker, which only it can see.
var remoteInventoryClient = &http.Client{Timeout: 10 * time.Second}

// Inventory asks the backend's agent which executions its host holds.
func (r *Remote) Inventory(ctx context.Context) ([]ExecutionInventory, error) {
	var body struct {
		Executions []ExecutionInventory `json:"executions"`
	}
	if err := r.agentCall(ctx, http.MethodGet, "/api/v1/agent/inventory", &body); err != nil {
		return nil, err
	}
	return body.Executions, nil
}

// Reap asks the backend's agent to remove an execution from its host — what
// its release job does when the execution ends.
func (r *Remote) Reap(ctx context.Context, runID string) error {
	return r.agentCall(ctx, http.MethodPost, "/api/v1/agent/executions/"+url.PathEscape(runID)+"/reap", nil)
}

func (r *Remote) agentCall(ctx context.Context, method, path string, out any) error {
	base := strings.TrimRight(strings.TrimSpace(r.baseURL), "/")
	if base == "" {
		return errors.New("the backend has no hostUrl for its agent")
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return err
	}
	if r.secret != "" {
		req.Header.Set("Authorization", "Bearer "+r.secret)
	}
	res, err := remoteInventoryClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= http.StatusBadRequest {
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("agent returned %d: %s", res.StatusCode, strings.TrimSpace(string(detail)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
