package auth

import (
	"sync"
	"time"
)

const (
	lockoutMaxFailures = 10
	lockoutWindow      = 10 * time.Minute
	lockoutSweepEvery  = 200
)

type failureRecord struct {
	times []time.Time
}

type accountLockout struct {
	mu        sync.Mutex
	failures  map[string]*failureRecord
	callCount int
}

func newAccountLockout() *accountLockout {
	return &accountLockout{failures: make(map[string]*failureRecord)}
}

// locked reports whether the account identified by key has exceeded the
// allowed failure count within the rolling window.
func (a *accountLockout) locked(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.recentFailures(key) >= lockoutMaxFailures
}

// record adds a failed attempt for key.
func (a *accountLockout) record(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.callCount++
	if a.callCount >= lockoutSweepEvery {
		a.sweepLocked()
		a.callCount = 0
	}

	rec := a.failures[key]
	if rec == nil {
		rec = &failureRecord{}
		a.failures[key] = rec
	}
	rec.times = append(rec.times, time.Now())
}

// reset clears the failure history for key (called after a successful sign-in).
func (a *accountLockout) reset(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.failures, key)
}

func (a *accountLockout) recentFailures(key string) int {
	rec := a.failures[key]
	if rec == nil {
		return 0
	}
	cutoff := time.Now().Add(-lockoutWindow)
	count := 0
	for _, t := range rec.times {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

func (a *accountLockout) sweepLocked() {
	cutoff := time.Now().Add(-lockoutWindow)
	for key, rec := range a.failures {
		var fresh []time.Time
		for _, t := range rec.times {
			if t.After(cutoff) {
				fresh = append(fresh, t)
			}
		}
		if len(fresh) == 0 {
			delete(a.failures, key)
		} else {
			rec.times = fresh
		}
	}
}
