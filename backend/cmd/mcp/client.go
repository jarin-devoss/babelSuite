package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v5"
)

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetry   = 5 * time.Minute
	userAgent         = "babelsuite-mcp/1.0"
	watchPollInitial  = 2 * time.Second
	watchPollMax      = 30 * time.Second
	watchDefaultLimit = 30 * time.Minute

	// Mirrors httpserver.CSRFCookieName / CSRFHeaderName.
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"

	// Any safe method is enough to be issued a CSRF cookie; this one is
	// reachable without a token.
	csrfPrimePath = "/api/v1/auth/config"
)

type clientOption func(*client) error

func withTimeout(d time.Duration) clientOption {
	return func(c *client) error {
		c.http.Timeout = d
		return nil
	}
}

type client struct {
	baseURL string
	http    *http.Client

	// The stdio server dispatches tool calls on worker goroutines, so the
	// sign_in tool can be storing a token while another call is reading it.
	authMu sync.RWMutex
	token  string
	// startupAuthErr records why the automatic sign-in failed, so a later
	// "Sign in required" can name the real cause instead of looking like
	// rejected credentials.
	startupAuthErr error
}

// setToken stores the bearer token used by subsequent requests.
func (c *client) setToken(token string) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.token = token
}

// setStartupAuthErr records why the automatic sign-in failed.
func (c *client) setStartupAuthErr(err error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.startupAuthErr = err
}

// authState returns the current token and the startup failure together so a
// caller sees a consistent pair.
func (c *client) authState() (string, error) {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.token, c.startupAuthErr
}

func newClient(baseURL, token string, opts ...clientOption) *client {
	// The jar keeps the CSRF cookie the control plane issues on safe methods so
	// it can be echoed back on unauthenticated writes.
	jar, _ := cookiejar.New(nil)
	c := &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: defaultTimeout, Jar: jar},
	}
	for _, opt := range opts {
		_ = opt(c)
	}
	return c
}

// csrfToken returns the CSRF cookie the control plane issued for u, if any.
func (c *client) csrfToken(u *url.URL) string {
	if c.http.Jar == nil || u == nil {
		return ""
	}
	for _, cookie := range c.http.Jar.Cookies(u) {
		if cookie.Name == csrfCookieName {
			return cookie.Value
		}
	}
	return ""
}

// ensureCSRFCookie fetches a CSRF cookie when the next request will be a write
// that carries no bearer token. Requests that do carry one are exempt from the
// CSRF check, so this is only ever needed for sign-in.
func (c *client) ensureCSRFCookie(ctx context.Context) {
	if token, _ := c.authState(); token != "" {
		return
	}
	u, err := url.Parse(c.baseURL + csrfPrimePath)
	if err != nil || c.csrfToken(u) != "" {
		return
	}
	// A failure here is not fatal: the write still goes out and reports the
	// real error from the control plane rather than a misleading one.
	_, _ = c.get(ctx, csrfPrimePath, nil)
}

func (c *client) get(ctx context.Context, path string, queryParams map[string]string) (json.RawMessage, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, backoff.Permanent(err)
	}
	if len(queryParams) > 0 {
		q := u.Query()
		for k, v := range queryParams {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}
	return backoff.Retry(ctx, func() (json.RawMessage, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, backoff.Permanent(err)
		}
		return c.do(req)
	}, backoff.WithBackOff(newBackoff()), backoff.WithMaxElapsedTime(defaultMaxRetry))
}

func (c *client) post(ctx context.Context, path string, body any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, err
	}
	raw := buf.Bytes()

	c.ensureCSRFCookie(ctx)

	return backoff.Retry(ctx, func() (json.RawMessage, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
		if err != nil {
			return nil, backoff.Permanent(err)
		}
		req.Header.Set("Content-Type", "application/json")
		return c.do(req)
	}, backoff.WithBackOff(newBackoff()), backoff.WithMaxElapsedTime(defaultMaxRetry))
}

func (c *client) put(ctx context.Context, path string, body any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, err
	}
	raw := buf.Bytes()

	c.ensureCSRFCookie(ctx)

	return backoff.Retry(ctx, func() (json.RawMessage, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(raw))
		if err != nil {
			return nil, backoff.Permanent(err)
		}
		req.Header.Set("Content-Type", "application/json")
		return c.do(req)
	}, backoff.WithBackOff(newBackoff()), backoff.WithMaxElapsedTime(defaultMaxRetry))
}

func (c *client) delete(ctx context.Context, path string) (json.RawMessage, error) {
	c.ensureCSRFCookie(ctx)

	return backoff.Retry(ctx, func() (json.RawMessage, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
		if err != nil {
			return nil, backoff.Permanent(err)
		}
		result, err := c.do(req)
		if err != nil {
			return nil, err
		}
		return result, nil
	}, backoff.WithBackOff(newBackoff()), backoff.WithMaxElapsedTime(defaultMaxRetry))
}

func (c *client) do(req *http.Request) (json.RawMessage, error) {
	req.Header.Set("User-Agent", userAgent)
	token, startupAuthErr := c.authState()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if req.Method != http.MethodGet && req.Method != http.MethodHead {
		// Unauthenticated writes have to echo the CSRF cookie back in a header.
		if token := c.csrfToken(req.URL); token != "" {
			req.Header.Set(csrfHeaderName, token)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// Network errors are transient — let the caller retry.
		return nil, err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	tee := io.TeeReader(resp.Body, &buf)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(tee)
		msg := extractErrorMessage(data, resp.StatusCode)

		// Being told to sign in when the startup sign-in itself failed reads as
		// bad credentials unless the original reason comes with it.
		if resp.StatusCode == http.StatusUnauthorized && token == "" && startupAuthErr != nil {
			msg = fmt.Sprintf("%s (automatic sign-in at startup failed: %v)", msg, startupAuthErr)
		}

		apiErr := fmt.Errorf("%s", msg)

		// Rate limiting is transient, so let the backoff ride it out rather
		// than reporting it as a permanent failure.
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, apiErr
		}
		// Other 4xx are permanent: retrying won't help.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return nil, backoff.Permanent(apiErr)
		}
		return nil, apiErr
	}

	var result json.RawMessage
	if err := json.NewDecoder(tee).Decode(&result); err != nil && err != io.EOF {
		return nil, backoff.Permanent(fmt.Errorf("decode response: %s: %s", err, buf.String()))
	}
	return result, nil
}

// watchExecution polls GET /api/v1/executions/{id} until the execution reaches
// a terminal status (healthy or failed) or the context deadline is exceeded.
// The poll interval grows with exponential backoff starting at watchPollInitial.
func (c *client) watchExecution(ctx context.Context, id string, timeout time.Duration) (json.RawMessage, error) {
	if timeout <= 0 {
		timeout = watchDefaultLimit
	}
	deadline := time.Now().Add(timeout)

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = watchPollInitial
	b.MaxInterval = watchPollMax
	b.Reset()

	for {
		data, err := c.get(ctx, "/api/v1/executions/"+id, nil)
		if err != nil {
			return nil, err
		}

		var record struct {
			Status string `json:"status"`
		}
		// The control plane reports "Healthy"/"Failed"; match without regard to
		// case so the watch ends when the run does instead of polling to its
		// deadline.
		if json.Unmarshal(data, &record) == nil && isTerminalStatus(record.Status) {
			return data, nil
		}

		if time.Now().After(deadline) {
			return data, fmt.Errorf("watch timed out after %s", timeout)
		}

		wait := b.NextBackOff()
		if wait == backoff.Stop {
			return data, fmt.Errorf("watch backoff exhausted")
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// isTerminalStatus reports whether an execution has finished. Status casing has
// differed between the API and this client before, so compare case-insensitively.
func isTerminalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "healthy", "failed":
		return true
	}
	return false
}

func (c *client) signIn(ctx context.Context, email, password string) (string, error) {
	data, err := c.post(ctx, "/api/v1/auth/sign-in", map[string]string{"email": email, "password": password})
	if err != nil {
		return "", err
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	return resp.Token, nil
}

func newBackoff() *backoff.ExponentialBackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 100 * time.Millisecond
	b.MaxInterval = 10 * time.Second
	b.Reset()
	return b
}

func extractErrorMessage(data []byte, statusCode int) string {
	var raw map[string]any
	if json.Unmarshal(data, &raw) == nil {
		if msg, ok := raw["error"].(string); ok && msg != "" {
			return fmt.Sprintf("API error %d: %s", statusCode, msg)
		}
		if msg, ok := raw["message"].(string); ok && msg != "" {
			return fmt.Sprintf("API error %d: %s", statusCode, msg)
		}
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return fmt.Sprintf("API error %d", statusCode)
	}
	return fmt.Sprintf("API error %d: %s", statusCode, body)
}
