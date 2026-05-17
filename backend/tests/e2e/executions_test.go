//go:build e2e

package e2e

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func registerSuite(t *testing.T, h *harness) string {
	t.Helper()
	id := uniqueID("suite")
	resp, err := h.admin.postRaw("/api/v1/suites", map[string]string{
		"id":        id,
		"suiteStar": minimalSuiteStar(),
	})
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	return id
}

func TestRegisteredSuiteAppearsInLaunchSuites(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	id := registerSuite(t, h)

	var body struct {
		Suites []struct {
			ID string `json:"id"`
		} `json:"suites"`
	}
	h.admin.getJSON(t, "/api/v1/executions/launch-suites", &body)

	for _, s := range body.Suites {
		if s.ID == id {
			return
		}
	}
	r.Failf("not found", "suite %q not in launch-suites (%d entries)", id, len(body.Suites))
}

func TestCreateExecution(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	id := registerSuite(t, h)

	var execution struct {
		ID      string `json:"id"`
		SuiteID string `json:"suiteId"`
		Status  string `json:"status"`
	}
	h.admin.mustPostJSON(t, "/api/v1/executions", map[string]string{"suiteId": id}, &execution)
	r.NotEmpty(execution.ID)
	r.Equal(id, execution.SuiteID)
	r.NotEmpty(execution.Status)
}

func TestGetExecution(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	suiteID := registerSuite(t, h)

	var created struct {
		ID string `json:"id"`
	}
	h.admin.mustPostJSON(t, "/api/v1/executions", map[string]string{"suiteId": suiteID}, &created)
	r.NotEmpty(created.ID)

	var fetched struct {
		ID    string `json:"id"`
		Suite struct {
			ID string `json:"id"`
		} `json:"suite"`
	}
	h.admin.getJSON(t, "/api/v1/executions/"+created.ID, &fetched)
	r.Equal(created.ID, fetched.ID)
	r.Equal(suiteID, fetched.Suite.ID)
}

func TestCreatedExecutionAppearsInList(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	suiteID := registerSuite(t, h)

	var created struct {
		ID string `json:"id"`
	}
	h.admin.mustPostJSON(t, "/api/v1/executions", map[string]string{"suiteId": suiteID}, &created)

	var list struct {
		Executions []struct {
			ID string `json:"id"`
		} `json:"executions"`
	}
	h.admin.getJSON(t, "/api/v1/executions", &list)

	for _, e := range list.Executions {
		if e.ID == created.ID {
			return
		}
	}
	r.Failf("not found", "execution %q not found in list (%d entries)", created.ID, len(list.Executions))
}

func TestCreateExecutionUnknownSuite(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.postRaw("/api/v1/executions", map[string]string{
		"suiteId": "no-such-suite-" + randHex(),
	})
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}

func TestGetExecutionUnknown(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.getRaw("/api/v1/executions/no-such-execution-" + randHex())
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}

func TestGetExecutionsOverview(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.getRaw("/api/v1/executions/overview")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

func TestResolveRef(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	id := registerSuite(t, h)

	var body struct {
		ID string `json:"id"`
	}
	h.admin.getJSON(t, "/api/v1/executions/resolve-ref?ref="+id, &body)
	r.Equal(id, body.ID)
}

func TestResolveRefMissingParam(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.getRaw("/api/v1/executions/resolve-ref")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusBadRequest)
}

func TestResolveRefUnknown(t *testing.T) {
	h := newHarness(t)

	resp, err := h.admin.getRaw("/api/v1/executions/resolve-ref?ref=no-such-suite-" + randHex())
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusNotFound)
}

func TestExecutionEventStreamDelivery(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	suiteID := registerSuite(t, h)

	var created struct {
		ID string `json:"id"`
	}
	h.admin.mustPostJSON(t, "/api/v1/executions", map[string]string{"suiteId": suiteID}, &created)
	r.NotEmpty(created.ID)

	req, err := http.NewRequest(http.MethodGet, h.url+"/api/v1/executions/"+created.ID+"/events", nil)
	r.NoError(err)
	req.Header.Set("Authorization", "Bearer "+h.admin.token)
	req.Header.Set("Accept", "text/event-stream")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	r.NoError(err)
	defer resp.Body.Close()
	r.Equal(http.StatusOK, resp.StatusCode)
	r.Equal("text/event-stream", strings.Split(resp.Header.Get("Content-Type"), ";")[0])

	scanner := bufio.NewScanner(resp.Body)
	received := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			received = true
			break
		}
	}
	r.True(received, "expected at least one SSE data frame from the event stream")
}

func TestExecutionLogStreamDelivery(t *testing.T) {
	h := newHarness(t)
	r := require.New(t)

	suiteID := registerSuite(t, h)

	var created struct {
		ID string `json:"id"`
	}
	h.admin.mustPostJSON(t, "/api/v1/executions", map[string]string{"suiteId": suiteID}, &created)
	r.NotEmpty(created.ID)

	req, err := http.NewRequest(http.MethodGet, h.url+"/api/v1/executions/"+created.ID+"/logs", nil)
	r.NoError(err)
	req.Header.Set("Authorization", "Bearer "+h.admin.token)
	req.Header.Set("Accept", "text/event-stream")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	r.NoError(err)
	defer resp.Body.Close()
	r.Equal(http.StatusOK, resp.StatusCode)
}
