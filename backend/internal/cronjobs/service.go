package cronjobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

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

type Service struct {
	store    Store
	smtp     SMTPConfig
	cr       *cron.Cron
	mu       sync.Mutex
	entryIDs map[string]cron.EntryID
}

func NewService(store Store, smtp SMTPConfig) *Service {
	return &Service{
		store:    store,
		smtp:     smtp,
		cr:       cron.New(),
		entryIDs: make(map[string]cron.EntryID),
	}
}

func (s *Service) Start(ctx context.Context) error {
	jobs, err := s.store.ListCronJobs(ctx)
	if err != nil {
		return err
	}
	for i := range jobs {
		if jobs[i].Enabled {
			_ = s.schedule(&jobs[i])
		}
	}
	s.cr.Start()
	return nil
}

func (s *Service) Stop() {
	s.cr.Stop()
}

func (s *Service) List(ctx context.Context) ([]CronJob, error) {
	return s.store.ListCronJobs(ctx)
}

func (s *Service) Get(ctx context.Context, id string) (*CronJob, error) {
	return s.store.GetCronJob(ctx, id)
}

func (s *Service) Create(ctx context.Context, job *CronJob) (*CronJob, error) {
	now := time.Now().UTC()
	job.ID = uuid.NewString()
	job.CreatedAt = now
	job.UpdatedAt = now
	if err := s.store.CreateCronJob(ctx, job); err != nil {
		return nil, err
	}
	if job.Enabled {
		_ = s.schedule(job)
	}
	return job, nil
}

func (s *Service) Update(ctx context.Context, id string, patch *CronJob) (*CronJob, error) {
	existing, err := s.store.GetCronJob(ctx, id)
	if err != nil {
		return nil, err
	}
	patch.ID = existing.ID
	patch.CreatedAt = existing.CreatedAt
	patch.UpdatedAt = time.Now().UTC()
	patch.LastRunAt = existing.LastRunAt
	patch.NextRunAt = existing.NextRunAt
	patch.LastError = existing.LastError
	if err := s.store.UpdateCronJob(ctx, patch); err != nil {
		return nil, err
	}
	s.unschedule(id)
	if patch.Enabled {
		_ = s.schedule(patch)
	}
	return patch, nil
}

func (s *Service) Delete(ctx context.Context, id string) error {
	s.unschedule(id)
	return s.store.DeleteCronJob(ctx, id)
}

func (s *Service) schedule(job *CronJob) error {
	id, err := s.cr.AddFunc(job.Schedule, s.makeRunner(job.ID))
	if err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", job.Schedule, err)
	}
	s.mu.Lock()
	s.entryIDs[job.ID] = id
	s.mu.Unlock()

	entry := s.cr.Entry(id)
	if entry.ID != 0 {
		next := entry.Next
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		j, err2 := s.store.GetCronJob(ctx, job.ID)
		if err2 == nil {
			j.NextRunAt = &next
			_ = s.store.UpdateCronJob(ctx, j)
		}
	}
	return nil
}

func (s *Service) unschedule(jobID string) {
	s.mu.Lock()
	entryID, ok := s.entryIDs[jobID]
	if ok {
		delete(s.entryIDs, jobID)
	}
	s.mu.Unlock()
	if ok {
		s.cr.Remove(entryID)
	}
}

func (s *Service) makeRunner(jobID string) func() {
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		job, err := s.store.GetCronJob(ctx, jobID)
		if err != nil {
			return
		}

		runErr := s.dispatch(job)

		now := time.Now().UTC()
		job.LastRunAt = &now
		if runErr != nil {
			job.LastError = runErr.Error()
		} else {
			job.LastError = ""
		}
		s.mu.Lock()
		entryID, ok := s.entryIDs[jobID]
		s.mu.Unlock()
		if ok {
			entry := s.cr.Entry(entryID)
			if entry.ID != 0 {
				job.NextRunAt = &entry.Next
			}
		}
		_ = s.store.UpdateCronJob(ctx, job)
	}
}

func (s *Service) dispatch(job *CronJob) error {
	var errs []string
	if len(job.Email.Recipients) > 0 {
		if err := s.sendEmail(job); err != nil {
			errs = append(errs, "email: "+err.Error())
		}
	}
	if job.Slack.WebhookURL != "" {
		if err := s.sendSlack(job); err != nil {
			errs = append(errs, "slack: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Service) sendEmail(job *CronJob) error {
	if s.smtp.Host == "" {
		return fmt.Errorf("SMTP not configured")
	}
	addr := fmt.Sprintf("%s:%d", s.smtp.Host, s.smtp.Port)
	var auth smtp.Auth
	if s.smtp.Username != "" {
		auth = smtp.PlainAuth("", s.smtp.Username, s.smtp.Password, s.smtp.Host)
	}
	from := s.smtp.From
	if from == "" {
		from = s.smtp.Username
	}
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		from,
		strings.Join(job.Email.Recipients, ", "),
		job.Email.Subject,
		job.Email.Body,
	)
	return smtp.SendMail(addr, auth, from, job.Email.Recipients, []byte(msg))
}

func (s *Service) sendSlack(job *CronJob) error {
	payload, err := json.Marshal(map[string]string{"text": job.Slack.Message})
	if err != nil {
		return err
	}
	resp, err := http.Post(job.Slack.WebhookURL, "application/json", bytes.NewReader(payload)) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}
	return nil
}
