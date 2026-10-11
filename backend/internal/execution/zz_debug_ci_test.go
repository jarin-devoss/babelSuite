package execution

import (
	"context"
	"testing"
	"time"
)

// Temporary CI probe: prints every log line an execution produced, so a
// failure that only reproduces on the CI runner can be read from its output.
func dumpExecutionLogs(t *testing.T, service *Service, executionID string) {
	t.Helper()

	snapshot, err := service.GetExecution(executionID, "")
	if err != nil {
		t.Logf("PROBE get execution: %v", err)
		return
	}
	t.Logf("PROBE status=%q", snapshot.Status)
	for _, event := range snapshot.Events {
		t.Logf("PROBE event [%s/%s] %s", event.Source, event.Level, event.Text)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := service.SubscribeLogs(ctx, executionID, "", 0)
	if err != nil {
		t.Logf("PROBE subscribe logs: %v", err)
		return
	}
	deadline := time.After(4 * time.Second)
	for count := 0; count < 300; count++ {
		select {
		case record, ok := <-stream:
			if !ok {
				return
			}
			t.Logf("PROBE log [%s/%s] %s", record.Line.Source, record.Line.Level, record.Line.Text)
		case <-deadline:
			return
		}
	}
}
