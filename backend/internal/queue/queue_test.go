package queue

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestMemoryQueueHonorsDependencies(t *testing.T) {
	q := NewMemory(context.Background())
	defer q.Close()

	var mu sync.Mutex
	order := []string{}
	doneCh := make(chan struct{})

	err := q.Enqueue([]Task{
		{
			ID: "first",
			Run: func(context.Context) error {
				mu.Lock()
				order = append(order, "first")
				mu.Unlock()
				return nil
			},
		},
		{
			ID:           "second",
			Dependencies: []string{"first"},
			Run: func(context.Context) error {
				mu.Lock()
				order = append(order, "second")
				mu.Unlock()
				close(doneCh)
				return nil
			},
		},
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for dependent task")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("unexpected execution order: %#v", order)
	}
}

// A pool never runs more tasks than its capacity, and a pool with no limit
// is not held back by one that has one — the reason a fixed worker count
// shared by every backend was replaced.
func TestMemoryQueueHonorsPoolCapacity(t *testing.T) {
	q := NewMemory(context.Background())
	defer q.Close()

	var mu sync.Mutex
	running, peak := map[string]int{}, map[string]int{}
	release := make(chan struct{})
	var wg sync.WaitGroup

	task := func(id, pool string, capacity int) Task {
		wg.Add(1)
		return Task{
			ID:       id,
			Pool:     pool,
			Capacity: func() int { return capacity },
			Run: func(context.Context) error {
				defer wg.Done()
				mu.Lock()
				running[pool]++
				if running[pool] > peak[pool] {
					peak[pool] = running[pool]
				}
				mu.Unlock()
				<-release
				mu.Lock()
				running[pool]--
				mu.Unlock()
				return nil
			},
		}
	}

	var tasks []Task
	for i := range 6 {
		tasks = append(tasks, task(fmt.Sprintf("agent-%d", i), "agent", 2))
		tasks = append(tasks, task(fmt.Sprintf("k8s-%d", i), "k8s", 0))
	}
	if err := q.Enqueue(tasks); err != nil {
		t.Fatal(err)
	}

	// Let every task that can start, start.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		started := running["agent"] == 2 && running["k8s"] == 6
		mu.Unlock()
		if started {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	wg.Wait()

	if peak["agent"] != 2 {
		t.Errorf("agent pool peaked at %d running, want its capacity 2", peak["agent"])
	}
	if peak["k8s"] != 6 {
		t.Errorf("unlimited pool peaked at %d, want all 6 at once", peak["k8s"])
	}
}
