package auth

import (
	"testing"
	"time"
)

func TestLockoutNotLockedInitially(t *testing.T) {
	t.Parallel()
	a := newAccountLockout()
	if a.locked("user@example.com") {
		t.Fatal("expected account to be unlocked initially")
	}
}

func TestLockoutLocksAfterMaxFailures(t *testing.T) {
	t.Parallel()
	a := newAccountLockout()
	email := "victim@example.com"
	for i := 0; i < lockoutMaxFailures; i++ {
		if a.locked(email) {
			t.Fatalf("locked too early after %d failures", i)
		}
		a.record(email)
	}
	if !a.locked(email) {
		t.Fatal("expected account to be locked after max failures")
	}
}

func TestLockoutResetClearsFailures(t *testing.T) {
	t.Parallel()
	a := newAccountLockout()
	email := "reset@example.com"
	for i := 0; i < lockoutMaxFailures; i++ {
		a.record(email)
	}
	if !a.locked(email) {
		t.Fatal("expected locked before reset")
	}
	a.reset(email)
	if a.locked(email) {
		t.Fatal("expected unlocked after reset")
	}
}

func TestLockoutDoesNotAffectOtherAccounts(t *testing.T) {
	t.Parallel()
	a := newAccountLockout()
	for i := 0; i < lockoutMaxFailures; i++ {
		a.record("attacker@example.com")
	}
	if a.locked("innocent@example.com") {
		t.Fatal("lockout of one account must not affect other accounts")
	}
}

func TestLockoutSweepRemovesExpiredEntries(t *testing.T) {
	t.Parallel()
	a := newAccountLockout()
	email := "old@example.com"

	// Plant a failure with an old timestamp directly to bypass window.
	a.mu.Lock()
	a.failures[email] = &failureRecord{
		times: []time.Time{time.Now().Add(-(lockoutWindow + time.Second))},
	}
	a.mu.Unlock()

	// recentFailures should return 0 since the entry is outside the window.
	a.mu.Lock()
	count := a.recentFailures(email)
	a.mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 recent failures, got %d", count)
	}
}

func TestLockoutWindowExpiry(t *testing.T) {
	t.Parallel()
	a := newAccountLockout()
	email := "expire@example.com"

	// Plant failures just outside the window.
	old := time.Now().Add(-(lockoutWindow + time.Second))
	a.mu.Lock()
	rec := &failureRecord{}
	for i := 0; i < lockoutMaxFailures; i++ {
		rec.times = append(rec.times, old)
	}
	a.failures[email] = rec
	a.mu.Unlock()

	if a.locked(email) {
		t.Fatal("failures outside the window must not cause a lockout")
	}
}
