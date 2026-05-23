package cronjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/execution"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type executionRunner interface {
	CreateExecution(ctx context.Context, req execution.CreateRequest) (*execution.ExecutionSummary, error)
	GetExecution(executionID, workspaceID string) (*execution.ExecutionRecord, error)
}

type Service struct {
	store      Store
	smtpFn     func() SMTPConfig
	executions executionRunner
	runner     *cron.Cron
	entryIDs   map[string]cron.EntryID
}

func NewService(store Store, smtpFn func() SMTPConfig, executions executionRunner) *Service {
	return &Service{
		store:      store,
		smtpFn:     smtpFn,
		executions: executions,
		runner:     cron.New(),
		entryIDs:   make(map[string]cron.EntryID),
	}
}

func (s *Service) Start(ctx context.Context) error {
	jobs, err := s.store.ListCronJobs(ctx)
	if err != nil {
		return fmt.Errorf("loading cron jobs: %w", err)
	}
	for i := range jobs {
		if jobs[i].Enabled {
			if err := s.schedule(&jobs[i]); err != nil {
				slog.Warn("skipping invalid cron schedule", "job", jobs[i].ID, "schedule", jobs[i].Schedule, "err", err)
			}
		}
	}
	s.runner.Start()
	return nil
}

func (s *Service) Stop() {
	s.runner.Stop()
}

func (s *Service) List(ctx context.Context) ([]CronJob, error) {
	return s.store.ListCronJobs(ctx)
}

func (s *Service) Get(ctx context.Context, id string) (*CronJob, error) {
	return s.store.GetCronJob(ctx, id)
}

func (s *Service) Create(ctx context.Context, job *CronJob) (*CronJob, error) {
	now := time.Now().UTC()
	job.ID = uuid.New().String()
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.Email.Recipients == nil {
		job.Email.Recipients = []string{}
	}
	if job.Suites == nil {
		job.Suites = []SuiteTarget{}
	}
	if err := s.store.CreateCronJob(ctx, job); err != nil {
		return nil, err
	}
	if job.Enabled {
		if err := s.schedule(job); err != nil {
			slog.Warn("created job with invalid schedule", "job", job.ID, "err", err)
		}
	}
	return job, nil
}

func (s *Service) Update(ctx context.Context, job *CronJob) (*CronJob, error) {
	job.UpdatedAt = time.Now().UTC()
	if job.Email.Recipients == nil {
		job.Email.Recipients = []string{}
	}
	if job.Suites == nil {
		job.Suites = []SuiteTarget{}
	}
	s.unschedule(job.ID)
	if job.Enabled {
		if err := s.schedule(job); err != nil {
			slog.Warn("updated job with invalid schedule", "job", job.ID, "err", err)
		}
	}
	if err := s.store.UpdateCronJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	s.unschedule(id)
	return s.store.DeleteCronJob(ctx, id)
}

func (s *Service) schedule(job *CronJob) error {
	id, err := s.runner.AddFunc(job.Schedule, s.makeRunner(job.ID))
	if err != nil {
		return err
	}
	s.entryIDs[job.ID] = id
	return nil
}

func (s *Service) unschedule(jobID string) {
	if eid, ok := s.entryIDs[jobID]; ok {
		s.runner.Remove(eid)
		delete(s.entryIDs, jobID)
	}
}

func (s *Service) makeRunner(jobID string) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		job, err := s.store.GetCronJob(ctx, jobID)
		if err != nil {
			slog.Error("cron job not found at run time", "job", jobID)
			return
		}

		runErr := s.dispatch(ctx, job)

		now := time.Now().UTC()
		job.LastRunAt = &now
		if runErr != nil {
			job.LastError = runErr.Error()
			slog.Error("cron job dispatch failed", "job", jobID, "err", runErr)
		} else {
			job.LastError = ""
		}
		job.UpdatedAt = now
		if err := s.store.UpdateCronJob(ctx, job); err != nil {
			slog.Error("could not persist cron job run result", "job", jobID, "err", err)
		}
	}
}

type suiteResult struct {
	target SuiteTarget
	record *execution.ExecutionRecord
	err    error
}

func (s *Service) dispatch(ctx context.Context, job *CronJob) error {
	results := s.runSuites(ctx, job.Suites)
	body := formatResults(job.Name, results)

	var errs []string
	if len(job.Email.Recipients) > 0 && job.Email.Subject != "" {
		if err := s.sendEmail(job, body); err != nil {
			errs = append(errs, "email: "+err.Error())
		}
	}
	if job.Slack.WebhookURL != "" {
		if err := s.sendSlack(job, body); err != nil {
			errs = append(errs, "slack: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Service) runSuites(ctx context.Context, targets []SuiteTarget) []suiteResult {
	results := make([]suiteResult, len(targets))
	for i, target := range targets {
		results[i] = suiteResult{target: target}
		if s.executions == nil {
			results[i].err = fmt.Errorf("execution service not available")
			continue
		}
		summary, err := s.executions.CreateExecution(ctx, execution.CreateRequest{
			SuiteID:   target.SuiteID,
			Profile:   target.Profile,
			Backend:   target.BackendID,
		})
		if err != nil {
			results[i].err = err
			continue
		}
		record, err := s.waitForExecution(ctx, summary.ID, 10*time.Minute)
		if err != nil {
			results[i].err = err
			continue
		}
		results[i].record = record
	}
	return results
}

func (s *Service) waitForExecution(ctx context.Context, execID string, timeout time.Duration) (*execution.ExecutionRecord, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rec, err := s.executions.GetExecution(execID, "")
		if err != nil {
			return nil, err
		}
		if rec.Status == "Healthy" || rec.Status == "Failed" {
			return rec, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	return nil, fmt.Errorf("execution %s timed out after %s", execID, timeout)
}

func countSteps(rec *execution.ExecutionRecord) (healthy, failed int) {
	seen := make(map[string]string)
	for _, e := range rec.Events {
		if e.Source != "" && (e.Status == "healthy" || e.Status == "failed" || e.Status == "skipped") {
			seen[e.Source] = e.Status
		}
	}
	for _, status := range seen {
		switch status {
		case "healthy":
			healthy++
		case "failed":
			failed++
		}
	}
	return
}

func formatResults(jobName string, results []suiteResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Cron job report: %s\n", jobName))
	sb.WriteString(fmt.Sprintf("Run at: %s\n\n", time.Now().UTC().Format(time.RFC1123)))

	allHealthy := true
	for _, r := range results {
		if r.err != nil || (r.record != nil && r.record.Status == "Failed") {
			allHealthy = false
			break
		}
	}
	if allHealthy {
		sb.WriteString("Overall: All suites passed\n\n")
	} else {
		sb.WriteString("Overall: One or more suites failed\n\n")
	}

	for _, r := range results {
		label := r.target.SuiteID
		if r.target.Profile != "" {
			label += " [" + r.target.Profile + "]"
		}
		if r.err != nil {
			sb.WriteString(fmt.Sprintf("FAILED   %s\n  Error: %s\n", label, r.err))
			continue
		}
		title := r.record.Suite.Title
		if title == "" {
			title = label
		}
		healthy, failed := countSteps(r.record)
		sb.WriteString(fmt.Sprintf("%-8s %s [%s]\n  Steps: %d healthy, %d failed | Duration: %s\n",
			r.record.Status, title, r.target.Profile,
			healthy, failed, r.record.Duration))
	}
	return sb.String()
}

func (s *Service) sendEmail(job *CronJob, body string) error {
	cfg := s.smtpFn()
	if cfg.Host == "" {
		return fmt.Errorf("SMTP not configured")
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		cfg.From,
		strings.Join(job.Email.Recipients, ", "),
		job.Email.Subject,
		body,
	)
	return smtp.SendMail(addr, auth, cfg.From, job.Email.Recipients, []byte(msg))
}

func (s *Service) sendSlack(job *CronJob, body string) error {
	payload, err := json.Marshal(map[string]string{"text": body})
	if err != nil {
		return err
	}
	resp, err := http.Post(job.Slack.WebhookURL, "application/json", bytes.NewReader(payload)) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}
	return nil
}
