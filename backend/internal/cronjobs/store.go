package cronjobs

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("cron job not found")

type Store interface {
	ListCronJobs(ctx context.Context) ([]CronJob, error)
	GetCronJob(ctx context.Context, id string) (*CronJob, error)
	CreateCronJob(ctx context.Context, job *CronJob) error
	UpdateCronJob(ctx context.Context, job *CronJob) error
	DeleteCronJob(ctx context.Context, id string) error
}
