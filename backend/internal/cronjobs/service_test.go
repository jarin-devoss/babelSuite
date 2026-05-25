package cronjobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/execution"
)

// --- fake store ---

type fakeStore struct {
	mu   sync.Mutex
	jobs map[string]*CronJob
}

func newFakeStore() *fakeStore {
	return &fakeStore{jobs: make(map[string]*CronJob)}
}

func (s *fakeStore) ListCronJobs(_ context.Context) ([]CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CronJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		out = append(out, cp)
	}
	return out, nil
}

func (s *fakeStore) GetCronJob(_ context.Context, id string) (*CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *j
	return &cp, nil
}

func (s *fakeStore) CreateCronJob(_ context.Context, job *CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *job
	s.jobs[job.ID] = &cp
	return nil
}

func (s *fakeStore) UpdateCronJob(_ context.Context, job *CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *job
	s.jobs[job.ID] = &cp
	return nil
}

func (s *fakeStore) DeleteCronJob(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.jobs, id)
	return nil
}

// --- fake execution runner ---

type fakeExecRunner struct {
	mu      sync.Mutex
	created []execution.CreateRequest
	record  *execution.ExecutionRecord
	err     error
}

func (f *fakeExecRunner) CreateExecution(_ context.Context, req execution.CreateRequest) (*execution.ExecutionSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, req)
	if f.err != nil {
		return nil, f.err
	}
	return &execution.ExecutionSummary{ID: "exec-1"}, nil
}

func (f *fakeExecRunner) GetExecution(_, _ string) (*execution.ExecutionRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.record != nil {
		return f.record, nil
	}
	return &execution.ExecutionRecord{Status: "Healthy"}, nil
}

func noSMTP() SMTPConfig { return SMTPConfig{} }

// --- tests ---

func TestCreateAssignsIDAndPersists(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore(), noSMTP, nil)

	job, err := svc.Create(context.Background(), &CronJob{
		Name:     "nightly",
		Schedule: "@daily",
		Enabled:  false,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if job.ID == "" {
		t.Fatal("expected non-empty ID after create")
	}
	if job.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}
}

func TestCreateWithInvalidScheduleStillPersists(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := NewService(store, noSMTP, nil)

	job, err := svc.Create(context.Background(), &CronJob{
		Name:     "bad",
		Schedule: "not-a-cron",
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("create returned unexpected error: %v", err)
	}
	if job.ID == "" {
		t.Fatal("job should still be persisted despite bad schedule")
	}
}

func TestUpdateReschedulesJob(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore(), noSMTP, nil)

	job, _ := svc.Create(context.Background(), &CronJob{
		Name:     "hourly",
		Schedule: "@hourly",
		Enabled:  true,
	})

	job.Schedule = "@daily"
	updated, err := svc.Update(context.Background(), job)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Schedule != "@daily" {
		t.Fatalf("expected updated schedule, got %q", updated.Schedule)
	}
}

func TestDeleteRemovesJobFromStore(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	svc := NewService(store, noSMTP, nil)

	job, _ := svc.Create(context.Background(), &CronJob{Name: "tmp", Schedule: "@daily"})
	if err := svc.Delete(context.Background(), job.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := svc.Get(context.Background(), job.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestListReturnsPreviouslyCreatedJobs(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore(), noSMTP, nil)

	svc.Create(context.Background(), &CronJob{Name: "a", Schedule: "@daily"})
	svc.Create(context.Background(), &CronJob{Name: "b", Schedule: "@daily"})

	jobs, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestRunSuitesCallsExecutionRunner(t *testing.T) {
	t.Parallel()
	runner := &fakeExecRunner{}
	svc := NewService(newFakeStore(), noSMTP, runner)

	targets := []SuiteTarget{
		{SuiteID: "payment-suite", Profile: "default", BackendID: "local"},
	}
	results := svc.runSuites(context.Background(), targets)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].err != nil {
		t.Fatalf("unexpected suite run error: %v", results[0].err)
	}
	if len(runner.created) != 1 || runner.created[0].SuiteID != "payment-suite" {
		t.Fatal("expected CreateExecution to be called with the right suite ID")
	}
}

func TestRunSuitesReturnsErrorWhenExecutionFails(t *testing.T) {
	t.Parallel()
	runner := &fakeExecRunner{err: errors.New("backend unavailable")}
	svc := NewService(newFakeStore(), noSMTP, runner)

	results := svc.runSuites(context.Background(), []SuiteTarget{{SuiteID: "x"}})
	if results[0].err == nil {
		t.Fatal("expected error when execution runner fails")
	}
}

func TestRunSuitesWithNilRunnerReturnsError(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore(), noSMTP, nil)

	results := svc.runSuites(context.Background(), []SuiteTarget{{SuiteID: "x"}})
	if results[0].err == nil {
		t.Fatal("expected error when execution runner is nil")
	}
}

func TestWaitForExecutionReturnsRecordOnTerminalStatus(t *testing.T) {
	t.Parallel()
	runner := &fakeExecRunner{
		record: &execution.ExecutionRecord{Status: "Healthy"},
	}
	svc := NewService(newFakeStore(), noSMTP, runner)

	rec, err := svc.waitForExecution(context.Background(), "exec-1", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Status != "Healthy" {
		t.Fatalf("expected Healthy, got %q", rec.Status)
	}
}

func TestWaitForExecutionTimesOut(t *testing.T) {
	t.Parallel()
	runner := &fakeExecRunner{
		record: &execution.ExecutionRecord{Status: "Running"},
	}
	svc := NewService(newFakeStore(), noSMTP, runner)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := svc.waitForExecution(ctx, "exec-1", 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestSendSlackPostsToWebhook(t *testing.T) {
	t.Parallel()
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		received = r.URL.Path
	}))
	defer srv.Close()

	svc := NewService(newFakeStore(), noSMTP, nil)
	job := &CronJob{Slack: SlackConfig{WebhookURL: srv.URL + "/hook"}}
	if err := svc.sendSlack(job, "hello"); err != nil {
		t.Fatalf("sendSlack: %v", err)
	}
	if received != "/hook" {
		t.Fatalf("expected webhook path /hook, got %q", received)
	}
}

func TestSendSlackReturnsErrorOnNon2xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	svc := NewService(newFakeStore(), noSMTP, nil)
	job := &CronJob{Slack: SlackConfig{WebhookURL: srv.URL}}
	if err := svc.sendSlack(job, "test"); err == nil {
		t.Fatal("expected error for non-2xx webhook response")
	}
}

func TestFormatResultsAllHealthy(t *testing.T) {
	t.Parallel()
	results := []suiteResult{
		{
			target: SuiteTarget{SuiteID: "suite-a", Profile: "default"},
			record: &execution.ExecutionRecord{Status: "Healthy", Suite: execution.ExecutionSuite{Title: "Suite A"}},

		},
	}
	body := formatResults("nightly", results)
	if !contains(body, "All suites passed") {
		t.Fatalf("expected all-passed message, got:\n%s", body)
	}
}

func TestFormatResultsWithFailure(t *testing.T) {
	t.Parallel()
	results := []suiteResult{
		{
			target: SuiteTarget{SuiteID: "suite-a"},
			err:    errors.New("backend down"),
		},
	}
	body := formatResults("nightly", results)
	if !contains(body, "One or more suites failed") {
		t.Fatalf("expected failure message, got:\n%s", body)
	}
	if !contains(body, "backend down") {
		t.Fatalf("expected error detail, got:\n%s", body)
	}
}

func TestFormatResultsMixedStatuses(t *testing.T) {
	t.Parallel()
	results := []suiteResult{
		{
			target: SuiteTarget{SuiteID: "a", Profile: "p1"},
			record: &execution.ExecutionRecord{Status: "Healthy", Suite: execution.ExecutionSuite{Title: "Suite A"}},

		},
		{
			target: SuiteTarget{SuiteID: "b", Profile: "p2"},
			record: &execution.ExecutionRecord{Status: "Failed", Suite: execution.ExecutionSuite{Title: "Suite B"}},
		},
	}
	body := formatResults("mixed-run", results)
	if !contains(body, "One or more suites failed") {
		t.Fatalf("expected failure summary, got:\n%s", body)
	}
}

func TestCountStepsFromEvents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		events        []execution.ExecutionEvent
		wantHealthy   int
		wantFailed    int
	}{
		{
			name: "all healthy",
			events: []execution.ExecutionEvent{
				{Source: "db", Status: "healthy"},
				{Source: "api", Status: "healthy"},
			},
			wantHealthy: 2,
			wantFailed:  0,
		},
		{
			name: "mixed",
			events: []execution.ExecutionEvent{
				{Source: "db", Status: "healthy"},
				{Source: "api", Status: "failed"},
				{Source: "cache", Status: "skipped"},
			},
			wantHealthy: 1,
			wantFailed:  1,
		},
		{
			name: "last status wins for same source",
			events: []execution.ExecutionEvent{
				{Source: "db", Status: "healthy"},
				{Source: "db", Status: "failed"},
			},
			wantHealthy: 0,
			wantFailed:  1,
		},
		{
			name:        "empty events",
			events:      nil,
			wantHealthy: 0,
			wantFailed:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := &execution.ExecutionRecord{Events: tc.events}
			healthy, failed := countSteps(rec)
			if healthy != tc.wantHealthy || failed != tc.wantFailed {
				t.Errorf("countSteps() = (%d, %d), want (%d, %d)", healthy, failed, tc.wantHealthy, tc.wantFailed)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()
}

func TestSendEmailWithNoSMTPConfigReturnsError(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore(), noSMTP, nil)
	job := &CronJob{
		Email: EmailConfig{
			Recipients: []string{"ops@example.com"},
			Subject:    "Nightly run",
		},
	}
	if err := svc.sendEmail(job, "body"); err == nil {
		t.Fatal("expected error when SMTP host is empty")
	}
}

func TestDispatchSkipsEmailWhenNoRecipients(t *testing.T) {
	t.Parallel()
	slackCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		slackCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	runner := &fakeExecRunner{}
	svc := NewService(newFakeStore(), noSMTP, runner)

	job := &CronJob{
		Name:   "test",
		Suites: []SuiteTarget{{SuiteID: "x"}},
		Slack:  SlackConfig{WebhookURL: srv.URL},
	}

	if err := svc.dispatch(context.Background(), job); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if slackCalls != 1 {
		t.Fatalf("expected 1 Slack call, got %d", slackCalls)
	}
}

func TestStartLoadsAndSchedulesEnabledJobs(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.jobs["j1"] = &CronJob{ID: "j1", Name: "hourly", Schedule: "@hourly", Enabled: true}
	store.jobs["j2"] = &CronJob{ID: "j2", Name: "disabled", Schedule: "@daily", Enabled: false}

	svc := NewService(store, noSMTP, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	svc.Stop()

	if _, ok := svc.entryIDs["j1"]; !ok {
		t.Fatal("expected enabled job to be scheduled")
	}
	if _, ok := svc.entryIDs["j2"]; ok {
		t.Fatal("disabled job should not be scheduled")
	}
}

func TestStartWithInvalidScheduleWarnsAndContinues(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.jobs["bad"] = &CronJob{ID: "bad", Name: "bad", Schedule: "NOT_VALID", Enabled: true}
	store.jobs["ok"] = &CronJob{ID: "ok", Name: "ok", Schedule: "@hourly", Enabled: true}

	svc := NewService(store, noSMTP, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("start should not fail for invalid schedules: %v", err)
	}
	svc.Stop()

	if _, ok := svc.entryIDs["ok"]; !ok {
		t.Fatal("valid job should still be scheduled despite bad peer")
	}
}

func TestUnscheduleRemovesEntryAndIgnoresMissingID(t *testing.T) {
	t.Parallel()
	svc := NewService(newFakeStore(), noSMTP, nil)

	job, _ := svc.Create(context.Background(), &CronJob{
		Name: "to-remove", Schedule: "@hourly", Enabled: true,
	})
	svc.unschedule(job.ID)

	if _, ok := svc.entryIDs[job.ID]; ok {
		t.Fatal("entry should be removed after unschedule")
	}

	// Calling unschedule on an unknown ID must not panic.
	svc.unschedule(fmt.Sprintf("nonexistent-%d", time.Now().UnixNano()))
}
