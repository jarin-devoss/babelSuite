package logstream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSubscribeToMissingStreamReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	_, err := hub.Subscribe(context.Background(), "nonexistent", 0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSnapshotReturnsCopyOfLines(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	hub.Open("run-snap")
	hub.Append("run-snap", Line{Text: "first"})
	hub.Append("run-snap", Line{Text: "second"})

	snap, err := hub.Snapshot("run-snap")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snap) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(snap))
	}
	// Mutating the snapshot must not affect the hub.
	snap[0].Text = "mutated"
	snap2, _ := hub.Snapshot("run-snap")
	if snap2[0].Text == "mutated" {
		t.Fatal("snapshot should return an independent copy")
	}
}

func TestSnapshotOfMissingStreamReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	_, err := hub.Snapshot("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSinceSkipsAlreadySeenLines(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	hub.Open("run-since")
	hub.Append("run-since", Line{Text: "line-1"})
	hub.Append("run-since", Line{Text: "line-2"})
	hub.Append("run-since", Line{Text: "line-3"})

	// Subscribe starting after the second line (since=2 means skip IDs ≤ 2).
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stream, err := hub.Subscribe(ctx, "run-since", 2)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	select {
	case rec := <-stream:
		if rec.Line.Text != "line-3" {
			t.Fatalf("expected line-3, got %q", rec.Line.Text)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for partial replay")
	}
}

func TestMultipleSubscribersAllReceiveLiveAppend(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	hub.Open("run-multi")

	const n = 5
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	streams := make([]<-chan Record, n)
	for i := range n {
		s, err := hub.Subscribe(ctx, "run-multi", 0)
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		streams[i] = s
	}

	hub.Append("run-multi", Line{Text: "broadcast"})

	for i, s := range streams {
		select {
		case rec := <-s:
			if rec.Line.Text != "broadcast" {
				t.Errorf("subscriber %d: expected broadcast, got %q", i, rec.Line.Text)
			}
		case <-ctx.Done():
			t.Fatalf("subscriber %d timed out", i)
		}
	}
}

func TestContextCancellationCleansUpSubscriber(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	hub.Open("run-cancel")

	ctx, cancel := context.WithCancel(context.Background())

	_, err := hub.Subscribe(ctx, "run-cancel", 0)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Cancel and allow the cleanup goroutine to run.
	cancel()
	time.Sleep(50 * time.Millisecond)

	hub.mu.Lock()
	s := hub.streams["run-cancel"]
	subCount := len(s.subs)
	hub.mu.Unlock()

	if subCount != 0 {
		t.Fatalf("expected 0 subscribers after cancel, got %d", subCount)
	}
}

func TestConcurrentAppendsAndSubscribesAreRaceFree(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	hub.Open("run-race")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			hub.Append("run-race", Line{Text: "msg"})
		}(i)
	}
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub.Subscribe(ctx, "run-race", 0) //nolint:errcheck
		}()
	}
	wg.Wait()
}
