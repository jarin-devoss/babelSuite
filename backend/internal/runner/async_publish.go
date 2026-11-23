package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AsyncMessage names one async mock message and the broker it is published to.
type AsyncMessage struct {
	Surface   string
	Operation string
	Kind      string // kafka, rocketmq, mqtt or nats
	Port      int
	Topic     string
}

// AsyncPublisher is implemented by a backend that can have a suite's sidecar
// publish an async mock's message to a broker service of the run.
type AsyncPublisher interface {
	PublishAsyncMock(ctx context.Context, suiteID, profile, executionID, brokerNodeID string, message AsyncMessage) error
}

// asyncPublishPath is the sidecar's trigger route (apisix.AsyncPublishRoute).
const asyncPublishPath = "/_babelsuite/async/publish"

// PublishAsyncMock has the suite's sidecar publish a mock message to the broker
// container of a run. The sidecar dials the broker's address on the run's network
// rather than its name: it was started on the default network, so its resolver
// does not know the names of a run's services.
func (l *Local) PublishAsyncMock(ctx context.Context, suiteID, profile, executionID, brokerNodeID string, message AsyncMessage) error {
	sidecarURL := SuiteSidecarURL(suiteID, profile)
	if sidecarURL == "" {
		return fmt.Errorf("the suite's gateway is not running")
	}
	address, err := ContainerIPOnExecutionNetwork(ctx, executionID, brokerNodeID)
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]any{
		"suite":     suiteID,
		"surface":   message.Surface,
		"operation": message.Operation,
		"kind":      message.Kind,
		"host":      address,
		"port":      message.Port,
		"topic":     message.Topic,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL+asyncPublishPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+currentMockToken())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("the gateway did not answer: %w", err)
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("the gateway could not publish: %s", strings.TrimSpace(string(answer)))
	}
	return nil
}
