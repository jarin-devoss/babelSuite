package cronjobs

import "time"

type SuiteTarget struct {
	SuiteID   string `json:"suiteId"   bson:"suite_id"`
	Profile   string `json:"profile"   bson:"profile"`
	BackendID string `json:"backendId" bson:"backend_id"`
}

type EmailConfig struct {
	Recipients []string `json:"recipients" bson:"recipients"`
	Subject    string   `json:"subject"    bson:"subject"`
}

type SlackConfig struct {
	WebhookURL string `json:"webhookUrl" bson:"webhook_url"`
}

type CronJob struct {
	ID        string       `json:"id"        bson:"id"`
	Name      string       `json:"name"      bson:"name"`
	Schedule  string       `json:"schedule"  bson:"schedule"`
	Enabled   bool         `json:"enabled"   bson:"enabled"`
	Suites    []SuiteTarget `json:"suites"   bson:"suites"`
	Email     EmailConfig  `json:"email"     bson:"email"`
	Slack     SlackConfig  `json:"slack"     bson:"slack"`
	LastRunAt *time.Time   `json:"lastRunAt" bson:"last_run_at"`
	NextRunAt *time.Time   `json:"nextRunAt" bson:"next_run_at"`
	LastError string       `json:"lastError" bson:"last_error"`
	CreatedAt time.Time    `json:"createdAt" bson:"created_at"`
	UpdatedAt time.Time    `json:"updatedAt" bson:"updated_at"`
}
