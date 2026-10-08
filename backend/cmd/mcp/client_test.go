package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// csrfGuardedServer mimics httpserver.CSRFMiddleware: safe methods hand out the
// cookie, bearer-authenticated requests are exempt, and every other write must
// echo the cookie back in the header.
func csrfGuardedServer(t *testing.T, issued string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: issued, Path: "/"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"passwordAuthEnabled":true}`))
			return
		}

		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		if err != nil || cookie.Value == "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"CSRF token missing."}`))
			return
		}
		if r.Header.Get(csrfHeaderName) != cookie.Value {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"CSRF token invalid."}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"signed-in-token"}`))
	}))
}

func TestSignInSatisfiesCSRFChallenge(t *testing.T) {
	server := csrfGuardedServer(t, "cookie-value")
	defer server.Close()

	token, err := newClient(server.URL, "").signIn(context.Background(), "admin@babelsuite.test", "admin")
	if err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if token != "signed-in-token" {
		t.Fatalf("expected the issued token, got %q", token)
	}
}

func TestUnauthenticatedWriteEchoesTheIssuedCookie(t *testing.T) {
	var sentHeader, sentCookie string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: "abc123", Path: "/"})
			_, _ = w.Write([]byte(`{}`))
			return
		}
		sentHeader = r.Header.Get(csrfHeaderName)
		if cookie, err := r.Cookie(csrfCookieName); err == nil {
			sentCookie = cookie.Value
		}
		_, _ = w.Write([]byte(`{"token":"t"}`))
	}))
	defer server.Close()

	if _, err := newClient(server.URL, "").signIn(context.Background(), "a@b.test", "pw"); err != nil {
		t.Fatalf("sign-in: %v", err)
	}
	if sentCookie != "abc123" {
		t.Fatalf("cookie was not resent, got %q", sentCookie)
	}
	if sentHeader != "abc123" {
		t.Fatalf("header did not echo the cookie, got %q", sentHeader)
	}
}

func TestBearerRequestsSkipTheCSRFPrime(t *testing.T) {
	var gets, posts int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
		} else {
			posts++
			if r.Header.Get(csrfHeaderName) != "" {
				t.Errorf("bearer request should not carry a CSRF header")
			}
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	if _, err := newClient(server.URL, "a-token").post(context.Background(), "/api/v1/executions", map[string]string{}); err != nil {
		t.Fatalf("post: %v", err)
	}
	// A token already exempts the request, so priming would be a wasted
	// round trip on every single write.
	if gets != 0 {
		t.Fatalf("expected no prime request, got %d", gets)
	}
	if posts != 1 {
		t.Fatalf("expected exactly one write, got %d", posts)
	}
}

func TestSignInSurfacesRealAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: "v", Path: "/"})
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"Incorrect email or password."}`))
	}))
	defer server.Close()

	// A bad password must report itself rather than being masked by a CSRF
	// rejection the caller cannot act on.
	_, err := newClient(server.URL, "").signIn(context.Background(), "a@b.test", "wrong")
	if err == nil {
		t.Fatal("expected sign-in to fail")
	}
	if !strings.Contains(err.Error(), "Incorrect email or password") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The stdio server dispatches tool calls on worker goroutines, so sign_in can
// store a token while other calls are reading it. Without synchronisation this
// trips the race detector that CI runs.
func TestTokenIsSafeUnderConcurrentUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	c := newClient(server.URL, "")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			c.setToken(fmt.Sprintf("token-%d", n))
			c.setStartupAuthErr(fmt.Errorf("attempt %d", n))
		}(i)
		go func() {
			defer wg.Done()
			_, _ = c.get(context.Background(), "/api/v1/executions", nil)
		}()
	}
	wg.Wait()

	if token, _ := c.authState(); token == "" {
		t.Fatal("expected one of the concurrent writers to have stored a token")
	}
}
