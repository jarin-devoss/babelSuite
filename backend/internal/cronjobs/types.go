package cronjobs

import "time"

type EmailConfig struct {
	Recipients []string `json:"recipients" bson:"recipients"`
	Subject    string   `json:"subject"    bson:"subject"`
	Body       string   `json:"body"       bson:"body"`
}

type SlackConfig struct {
	WebhookURL string `json:"webhookUrl" bson:"webhook_url"`
	Message    string `json:"message"    bson:"message"`
}

type CronJob struct {
	ID        string      `json:"id"        bson:"id"`
	Name      string      `json:"name"      bson:"name"`
	Schedule  string      `json:"schedule"  bson:"schedule"`
	Enabled   bool        `json:"enabled"   bson:"enabled"`
	Email     EmailConfig `json:"email"     bson:"email"`
	Slack     SlackConfig `json:"slack"     bson:"slack"`
	LastRunAt *time.Time  `json:"lastRunAt" bson:"last_run_at"`
	NextRunAt *time.Time  `json:"nextRunAt" bson:"next_run_at"`
	LastError string      `json:"lastError" bson:"last_error"`
	CreatedAt time.Time   `json:"createdAt" bson:"created_at"`
	UpdatedAt time.Time   `json:"updatedAt" bson:"updated_at"`
}
