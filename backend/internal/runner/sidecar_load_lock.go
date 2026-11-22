package runner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/babelsuite/babelsuite/internal/logstream"
)

// A suite's sidecar serves every run of the suite and profile, and one load
// generator in it already uses its CPU. Two at once would each measure the
// other, so a traffic step and a flood step take turns on one sidecar. The
// scanner's other modes, plugins and mocks send little and stay concurrent.
var sidecarLoad = struct {
	mu      sync.Mutex
	holders map[string]string        // sidecar name -> who is generating load
	slots   map[string]chan struct{} // sidecar name -> one-slot semaphore
}{
	holders: map[string]string{},
	slots:   map[string]chan struct{}{},
}

// acquireSidecarLoad waits for the sidecar's load slot and returns the function
// that gives it back. A step that has to wait says who it waits for, and how
// long it waited once it starts. A cancelled step stops waiting.
func acquireSidecarLoad(ctx context.Context, step StepSpec, emit func(logstream.Line)) (func(), error) {
	name := SidecarContainerName(step.SuiteID, step.Profile)
	me := fmt.Sprintf("%s/%s", step.ExecutionID, step.Node.Name)

	sidecarLoad.mu.Lock()
	slot, ok := sidecarLoad.slots[name]
	if !ok {
		slot = make(chan struct{}, 1)
		sidecarLoad.slots[name] = slot
	}
	holder := sidecarLoad.holders[name]
	sidecarLoad.mu.Unlock()

	began := time.Now()
	select {
	case slot <- struct{}{}:
	default:
		emit(line(step, "info", fmt.Sprintf("[%s] Waiting for %s, which is generating load on this suite's sidecar; two load generators in one sidecar would measure each other.", step.Node.Name, holder)))
		select {
		case slot <- struct{}{}:
		case <-ctx.Done():
			return nil, fmt.Errorf("stopped while waiting for %s to finish generating load on the sidecar: %w", holder, ctx.Err())
		}
		emit(line(step, "info", fmt.Sprintf("[%s] The sidecar is free after %.1fs; starting.", step.Node.Name, time.Since(began).Seconds())))
	}

	sidecarLoad.mu.Lock()
	sidecarLoad.holders[name] = me
	sidecarLoad.mu.Unlock()
	return func() {
		sidecarLoad.mu.Lock()
		delete(sidecarLoad.holders, name)
		sidecarLoad.mu.Unlock()
		<-slot
	}, nil
}
