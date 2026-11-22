//go:build integration

package runner

import (
	"strings"
	"testing"
)

// ---- consumer-lag ----------------------------------------------------------

func TestIntegrationPluginConsumerLagFailsWhenTheGroupIsBehind(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "consumer-lag", "check_lag", map[string]any{
		"kafka_rest_url": stack.RestURL, "group": "billing", "max_lag": 5,
	})
	if run.Passed() {
		t.Fatalf("group billing lags 99, over max_lag=5: the step must fail. Log: %v", run.Log.texts())
	}
	if !run.HasFinding("consumer.lag orders/", "error") {
		t.Fatalf("expected a critical finding naming the partition, got %v", run.Findings())
	}
}

func TestIntegrationPluginConsumerLagPassesWithinTheLimit(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "consumer-lag", "check_lag", map[string]any{
		"kafka_rest_url": stack.RestURL, "group": "billing", "max_lag": 200,
	})
	if !run.Passed() {
		t.Fatalf("lag 99 is within max_lag=200: %v", run.Err)
	}
}

func TestIntegrationPluginConsumerLagSeverityWarnReportsWithoutBlocking(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "consumer-lag", "check_lag", map[string]any{
		"kafka_rest_url": stack.RestURL, "group": "billing", "max_lag": 5, "severity": "warn",
	})
	if !run.Passed() {
		t.Fatalf("severity=warn must not block: %v", run.Err)
	}
	if !run.HasFinding("consumer.lag orders/", "warn") {
		t.Fatalf("the finding must still be shown, at warn level: %v", run.Findings())
	}
}

func TestIntegrationPluginConsumerLagWatchNeverBlocks(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "consumer-lag", "watch_lag", map[string]any{
		"kafka_rest_url": stack.RestURL, "group": "billing", "max_lag": 5,
	})
	if !run.Passed() {
		t.Fatalf("watch_lag reports at warn and must not block: %v", run.Err)
	}
	if !run.HasFinding("consumer.lag orders/", "warn") {
		t.Fatalf("expected the warn finding: %v", run.Findings())
	}
}

func TestIntegrationPluginConsumerLagFailsAgainstAProxyThatServesNoLag(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "consumer-lag", "check_lag", map[string]any{
		"kafka_rest_url": stack.RedpandaURL, "group": "billing", "max_lag": 5,
	})
	if run.Passed() {
		t.Fatal("a proxy that cannot report lag must fail the step, not pass it with lag 0")
	}
	if !strings.Contains(run.Err.Error(), "does not serve consumer-group lag") {
		t.Fatalf("the failure must say why, got %v", run.Err)
	}
}

func TestIntegrationPluginConsumerLagFailsForAGroupThatNeverCommitted(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "consumer-lag", "check_lag", map[string]any{
		"kafka_rest_url": stack.RestURL, "group": "nobody-ever-consumed", "max_lag": 5,
	})
	if run.Passed() {
		t.Fatal("a group the cluster has never heard of is not caught up")
	}
}

func TestIntegrationPluginConsumerLagFailsOnAnUnexpectedShape(t *testing.T) {
	// The proxy answers 200 to everything, with nothing a lag check can read.
	url := stub(t, jsonHandler(`{}`))
	run := runPlugin(t, "consumer-lag", "check_lag", map[string]any{
		"kafka_rest_url": url, "group": "billing", "max_lag": 5,
	})
	if run.Passed() || !strings.Contains(run.Err.Error(), "unexpected response shape") {
		t.Fatalf("expected an unexpected-shape failure, got %v", run.Err)
	}
}

func TestIntegrationPluginConsumerLagRejectsAnUnknownOperation(t *testing.T) {
	startKafka(t)
	status, body := postPlugin(t, "consumer-lag", `{"op":"nope","config":{"kafka_rest_url":"http://x","group":"g"}}`)
	if status != 400 || !strings.Contains(body, "unknown op nope") {
		t.Fatalf("expected 400 naming the operation, got %d %s", status, body)
	}
}

func TestIntegrationPluginConsumerLagRefusesABadConfigBeforeAnyRequest(t *testing.T) {
	run := runPlugin(t, "consumer-lag", "check_lag", map[string]any{
		"kafka_rest_url": "http://nowhere:1", "group": "g", "max_lag": -1,
	})
	if run.Passed() || !strings.Contains(run.Err.Error(), "plugin consumer-lag config") {
		t.Fatalf("expected a schema failure before dispatch, got %v", run.Err)
	}
}

// ---- dlq-inspector ---------------------------------------------------------

func TestIntegrationPluginDlqFailsWhenMessagesAreWaiting(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "dlq-inspector", "inspect", map[string]any{
		"kafka_rest_url": stack.RestURL, "topic": "orders-dlq", "max_messages": 0,
	})
	if run.Passed() {
		t.Fatalf("orders-dlq holds 4 messages, over max_messages=0: %v", run.Log.texts())
	}
	if !run.HasFinding("dlq.messages orders-dlq", "error") {
		t.Fatalf("expected a critical finding, got %v", run.Findings())
	}
	found := false
	for _, text := range run.Findings() {
		if strings.Contains(text, "found=4") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the count must be 4 (summed over every partition), got %v", run.Findings())
	}
}

func TestIntegrationPluginDlqPassesWhenTheTopicIsEmpty(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "dlq-inspector", "inspect", map[string]any{
		"kafka_rest_url": stack.RestURL, "topic": "orders-empty", "max_messages": 0,
	})
	if !run.Passed() {
		t.Fatalf("an empty topic must pass: %v", run.Err)
	}
}

func TestIntegrationPluginDlqWatchNeverBlocks(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "dlq-inspector", "watch_dlq", map[string]any{
		"kafka_rest_url": stack.RestURL, "topic": "orders-dlq", "max_messages": 0,
	})
	if !run.Passed() || !run.HasFinding("dlq.messages orders-dlq", "warn") {
		t.Fatalf("watch_dlq must report at warn and pass: err=%v findings=%v", run.Err, run.Findings())
	}
}

func TestIntegrationPluginDlqInspectTopicsFlagsEachTopicOverTheLimit(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "dlq-inspector", "inspect_topics", map[string]any{
		"kafka_rest_url": stack.RestURL, "topics": []any{"orders-dlq", "orders-empty"}, "max_messages": 0,
	})
	if run.Passed() {
		t.Fatal("orders-dlq is over the limit")
	}
	if !run.HasFinding("dlq.messages orders-dlq", "") || run.HasFinding("dlq.messages orders-empty", "") {
		t.Fatalf("only the topic over the limit is a finding: %v", run.Findings())
	}
}

func TestIntegrationPluginDlqFailsAgainstAProxyThatServesNoOffsets(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "dlq-inspector", "inspect", map[string]any{
		"kafka_rest_url": stack.RedpandaURL, "topic": "orders-dlq", "max_messages": 0,
	})
	if run.Passed() || !strings.Contains(run.Err.Error(), "does not serve partition offsets") {
		t.Fatalf("expected a named failure, got %v", run.Err)
	}
}

func TestIntegrationPluginDlqFailsForATopicThatDoesNotExist(t *testing.T) {
	stack := startKafka(t)
	run := runPlugin(t, "dlq-inspector", "inspect", map[string]any{
		"kafka_rest_url": stack.RestURL, "topic": "no-such-topic", "max_messages": 0,
	})
	if run.Passed() || !strings.Contains(run.Err.Error(), "does not exist") {
		t.Fatalf("expected the topic to be named as missing, got %v", run.Err)
	}
}

func TestIntegrationPluginDlqFailsOnAnUnexpectedShape(t *testing.T) {
	url := stub(t, jsonHandler(`{}`))
	run := runPlugin(t, "dlq-inspector", "inspect", map[string]any{
		"kafka_rest_url": url, "topic": "orders-dlq", "max_messages": 0,
	})
	if run.Passed() {
		t.Fatal("a reply that is not a partition list must fail the step")
	}
}
