package eventstream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSubscribeToMissingStreamReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	hub := NewHub[string]()
	_, err := hub.Subscribe(context.Background(), "nonexistent", 0)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestLenReturnsZeroForUnknownKey(t *testing.T) {
	t.Parallel()
	hub := NewHub[int]()
	if n := hub.Len("missing"); n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestLenTracksAppendedItems(t *testing.T) {
	t.Parallel()
	hub := NewHub[string]()
	hub.Open("k")
	hub.Append("k", "a")
	hub.Append("k", "b")
	if n := hub.Len("k"); n != 2 {
		t.Fatalf("expected 2, got %d", n)
	}
}

func TestSinceSkipsAlreadyDeliveredItems(t *testing.T) {
	t.Parallel()
	hub := NewHub[string]()
	hub.Open("es-since")
	hub.Append("es-since", "one")
	hub.Append("es-since", "two")
	hub.Append("es-since", "three")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stream, err := hub.Subscribe(ctx, "es-since", 2)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	select {
	case rec := <-stream:
		if rec.Payload != "three" {
			t.Fatalf("expected three, got %q", rec.Payload)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for partial replay")
	}
}

func TestMultipleSubscribersReceiveLivePayload(t *testing.T) {
	t.Parallel()
	hub := NewHub[int]()
	hub.Open("es-multi")

	const n = 4
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	streams := make([]<-chan Record[int], n)
	for i := range n {
		s, err := hub.Subscribe(ctx, "es-multi", 0)
		if err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
		streams[i] = s
	}

	hub.Append("es-multi", 99)

	for i, s := range streams {
		select {
		case rec := <-s:
			if rec.Payload != 99 {
				t.Errorf("subscriber %d: expected 99, got %d", i, rec.Payload)
			}
		case <-ctx.Done():
			t.Fatalf("subscriber %d timed out", i)
		}
	}
}

func TestContextCancellationCleansUpSubscriber(t *testing.T) {
	t.Parallel()
	hub := NewHub[string]()
	hub.Open("es-cancel")

	ctx, cancel := context.WithCancel(context.Background())

	_, err := hub.Subscribe(ctx, "es-cancel", 0)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	cancel()
	time.Sleep(50 * time.Millisecond)

	hub.mu.Lock()
	s := hub.streams["es-cancel"]
	subCount := len(s.subs)
	hub.mu.Unlock()

	if subCount != 0 {
		t.Fatalf("expected 0 subscribers after cancel, got %d", subCount)
	}
}

func TestAppendReturnsRecordWithMonotonicID(t *testing.T) {
	t.Parallel()
	hub := NewHub[string]()
	hub.Open("es-id")

	r1 := hub.Append("es-id", "a")
	r2 := hub.Append("es-id", "b")
	r3 := hub.Append("es-id", "c")

	if r1.ID != 1 || r2.ID != 2 || r3.ID != 3 {
		t.Fatalf("expected IDs 1,2,3 got %d,%d,%d", r1.ID, r2.ID, r3.ID)
	}
}

func TestConcurrentAppendsAreRaceFree(t *testing.T) {
	t.Parallel()
	hub := NewHub[int]()
	hub.Open("es-race")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			hub.Append("es-race", v)
		}(i)
	}
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub.Subscribe(ctx, "es-race", 0) //nolint:errcheck
		}()
	}
	wg.Wait()
}
