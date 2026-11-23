package auth

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/domain"
	"github.com/babelsuite/babelsuite/internal/store"
)

// resetStore keeps reset tokens the way a real store does: a token is consumed
// once.
type resetStore struct {
	*stubStore
	mu     sync.Mutex
	tokens map[string]string
}

func (s *resetStore) StorePasswordResetToken(_ context.Context, userID, tokenHash string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[tokenHash] = userID
	return nil
}

func (s *resetStore) ConsumePasswordResetToken(_ context.Context, tokenHash, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tokens[tokenHash]; !ok {
		return store.ErrNotFound
	}
	delete(s.tokens, tokenHash)
	return nil
}

// fakeSMTP accepts one message and hands its body to the test.
func fakeSMTP(t *testing.T) (port int, messages <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	out := make(chan string, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				reader := bufio.NewReader(conn)
				write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
				write("220 fake ESMTP")
				var data bytes.Buffer
				inData := false
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					trimmed := strings.TrimRight(line, "\r\n")
					switch {
					case inData && trimmed == ".":
						inData = false
						out <- data.String()
						write("250 queued")
					case inData:
						data.WriteString(trimmed + "\n")
					case strings.HasPrefix(trimmed, "EHLO"), strings.HasPrefix(trimmed, "HELO"):
						write("250 hello")
					case strings.HasPrefix(trimmed, "DATA"):
						inData = true
						write("354 go ahead")
					case strings.HasPrefix(trimmed, "QUIT"):
						write("221 bye")
						return
					default:
						write("250 ok")
					}
				}
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, out
}

// Password-reset mail goes through the SMTP settings that are edited on the
// settings page, read at each request. The reset page used to be reachable while
// the endpoint answered 503 forever, because the auth config never got SMTP.
func TestPasswordResetMailIsSentThroughTheConfiguredSMTP(t *testing.T) {
	t.Parallel()
	port, messages := fakeSMTP(t)

	base := newStubStore()
	users := &resetStore{stubStore: base, tokens: map[string]string{}}
	base.usersByEmail["ada@example.com"] = &domain.User{UserID: "u1", Email: "ada@example.com"}

	var settings SMTPConfig
	handler := NewHandler(users, NewJWT("test-secret"), Config{
		FrontendURL:         "http://localhost:5173",
		PasswordAuthEnabled: true,
		SMTPProvider:        func() SMTPConfig { return settings },
	})
	mux := http.NewServeMux()
	handler.Register(mux)

	post := func(path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "192.0.2.10:1234"
		mux.ServeHTTP(rec, request)
		return rec
	}

	// No SMTP configured: the endpoint says so.
	if rec := post("/api/v1/auth/forgot-password", `{"email":"ada@example.com"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without SMTP: status %d, want 503", rec.Code)
	}

	// SMTP set on the settings page, with no restart.
	settings = SMTPConfig{Host: "127.0.0.1", Port: port, From: "BabelSuite <no-reply@example.com>"}
	if rec := post("/api/v1/auth/forgot-password", `{"email":"ada@example.com"}`); rec.Code != http.StatusOK {
		t.Fatalf("with SMTP: status %d: %s", rec.Code, rec.Body.String())
	}

	var mail string
	select {
	case mail = <-messages:
	case <-time.After(10 * time.Second):
		t.Fatal("no mail reached the SMTP server")
	}
	link := regexp.MustCompile(`http://localhost:5173/reset-password\?token=([A-Za-z0-9_-]+)`).FindStringSubmatch(mail)
	if link == nil {
		t.Fatalf("the mail has no reset link:\n%s", mail)
	}

	body, _ := json.Marshal(map[string]string{"token": link[1], "password": "Sup3rStrong!"})
	if rec := post("/api/v1/auth/reset-password", string(body)); rec.Code != http.StatusOK {
		t.Fatalf("the link did not reset the password: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("/api/v1/auth/reset-password", string(body)); rec.Code != http.StatusBadRequest {
		t.Fatalf("the link worked twice: status %d", rec.Code)
	}
}
