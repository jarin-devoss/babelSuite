package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/babelsuite/babelsuite/internal/agent"
	"github.com/babelsuite/babelsuite/internal/cronjobs"
	"github.com/babelsuite/babelsuite/internal/domain"
	"github.com/babelsuite/babelsuite/internal/execution"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(dsn string) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.ConnConfig.Tracer = newPGXTracer()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	st := &Store{pool: pool}
	if err := st.migrate(ctx); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) Close(_ context.Context) error {
	s.pool.Close()
	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS workspaces (
  workspace_id TEXT PRIMARY KEY,
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
  user_id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(workspace_id),
  username TEXT NOT NULL UNIQUE,
  email TEXT NOT NULL UNIQUE,
  full_name TEXT NOT NULL,
  is_admin BOOLEAN NOT NULL DEFAULT false,
  pass_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS favorite_packages (
  user_id TEXT NOT NULL REFERENCES users(user_id) ON DELETE CASCADE,
  package_id TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, package_id)
);

CREATE TABLE IF NOT EXISTS runtime_documents (
  key TEXT PRIMARY KEY,
  payload JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS executions (
  execution_id TEXT PRIMARY KEY,
  started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  record       JSONB NOT NULL,
  total        INT NOT NULL DEFAULT 0,
  completed    INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS execution_logs (
  execution_id TEXT PRIMARY KEY,
  payload      TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS password_reset_tokens (
  user_id    TEXT PRIMARY KEY REFERENCES users(user_id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_log (
  id           BIGSERIAL PRIMARY KEY,
  request_id   TEXT NOT NULL DEFAULT '',
  method       TEXT NOT NULL,
  path         TEXT NOT NULL,
  route        TEXT NOT NULL DEFAULT '',
  status       INT NOT NULL,
  duration_ms  BIGINT NOT NULL,
  remote_addr  TEXT NOT NULL DEFAULT '',
  user_id      TEXT NOT NULL DEFAULT '',
  workspace_id TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cron_jobs (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL DEFAULT '',
  schedule    TEXT NOT NULL DEFAULT '',
  enabled     BOOLEAN NOT NULL DEFAULT true,
  email       JSONB NOT NULL DEFAULT '{}',
  slack       JSONB NOT NULL DEFAULT '{}',
  last_run_at TIMESTAMPTZ,
  next_run_at TIMESTAMPTZ,
  last_error  TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}

func (s *Store) CreateWorkspace(ctx context.Context, workspace *domain.Workspace) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO workspaces (workspace_id, slug, name, created_at) VALUES ($1, $2, $3, $4)`,
		workspace.WorkspaceID, workspace.Slug, workspace.Name, workspace.CreatedAt,
	)
	return wrap(err)
}

func (s *Store) DeleteWorkspace(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM workspaces WHERE workspace_id = $1`, id)
	return wrap(err)
}

func (s *Store) GetWorkspaceByID(ctx context.Context, id string) (*domain.Workspace, error) {
	var workspace domain.Workspace
	err := s.pool.QueryRow(ctx,
		`SELECT workspace_id, slug, name, created_at FROM workspaces WHERE workspace_id = $1`,
		id,
	).Scan(&workspace.WorkspaceID, &workspace.Slug, &workspace.Name, &workspace.CreatedAt)
	return &workspace, wrap(err)
}

func (s *Store) GetWorkspaceBySlug(ctx context.Context, slug string) (*domain.Workspace, error) {
	var workspace domain.Workspace
	err := s.pool.QueryRow(ctx,
		`SELECT workspace_id, slug, name, created_at FROM workspaces WHERE slug = $1`,
		slug,
	).Scan(&workspace.WorkspaceID, &workspace.Slug, &workspace.Name, &workspace.CreatedAt)
	return &workspace, wrap(err)
}

func (s *Store) CreateUser(ctx context.Context, user *domain.User) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users (user_id, workspace_id, username, email, full_name, is_admin, pass_hash, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		user.UserID, user.WorkspaceID, user.Username, user.Email, user.FullName, user.IsAdmin, user.PassHash, user.CreatedAt,
	)
	return wrap(err)
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, workspace_id, username, email, full_name, is_admin, pass_hash, created_at FROM users WHERE user_id = $1`,
		id,
	).Scan(&user.UserID, &user.WorkspaceID, &user.Username, &user.Email, &user.FullName, &user.IsAdmin, &user.PassHash, &user.CreatedAt)
	return &user, wrap(err)
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, workspace_id, username, email, full_name, is_admin, pass_hash, created_at FROM users WHERE email = $1`,
		email,
	).Scan(&user.UserID, &user.WorkspaceID, &user.Username, &user.Email, &user.FullName, &user.IsAdmin, &user.PassHash, &user.CreatedAt)
	return &user, wrap(err)
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, workspace_id, username, email, full_name, is_admin, pass_hash, created_at FROM users WHERE username = $1`,
		username,
	).Scan(&user.UserID, &user.WorkspaceID, &user.Username, &user.Email, &user.FullName, &user.IsAdmin, &user.PassHash, &user.CreatedAt)
	return &user, wrap(err)
}

func (s *Store) StorePasswordResetToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at`,
		userID, tokenHash, expiresAt,
	)
	return wrap(err)
}

func (s *Store) ConsumePasswordResetToken(ctx context.Context, tokenHash, newPassHash string) error {
	var userID string
	err := s.pool.QueryRow(ctx,
		`DELETE FROM password_reset_tokens WHERE token_hash = $1 AND expires_at > now() RETURNING user_id`,
		tokenHash,
	).Scan(&userID)
	if err != nil {
		return wrap(err)
	}
	_, err = s.pool.Exec(ctx, `UPDATE users SET pass_hash = $1 WHERE user_id = $2`, newPassHash, userID)
	return wrap(err)
}

func (s *Store) ListFavoritePackageIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT package_id FROM favorite_packages WHERE user_id = $1 ORDER BY created_at DESC, package_id ASC`,
		userID,
	)
	if err != nil {
		return nil, wrap(err)
	}
	defer rows.Close()

	packageIDs := make([]string, 0)
	for rows.Next() {
		var packageID string
		if err := rows.Scan(&packageID); err != nil {
			return nil, err
		}
		if packageID != "" {
			packageIDs = append(packageIDs, packageID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return packageIDs, nil
}

func (s *Store) SaveFavoritePackage(ctx context.Context, favorite *domain.FavoritePackage) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO favorite_packages (user_id, package_id, created_at) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, package_id) DO NOTHING`,
		favorite.UserID, favorite.PackageID, favorite.CreatedAt,
	)
	return wrap(err)
}

func (s *Store) RemoveFavoritePackage(ctx context.Context, userID, packageID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM favorite_packages WHERE user_id = $1 AND package_id = $2`,
		userID, packageID,
	)
	return wrap(err)
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return store.ErrDuplicate
	}
	return err
}

func (s *Store) LoadAgentRuntime(ctx context.Context) (*agent.RuntimeState, error) {
	var state agent.RuntimeState
	ok, err := s.loadRuntimeDocument(ctx, "agent-runtime", &state)
	if err != nil {
		return nil, err
	}
	if !ok {
		return &agent.RuntimeState{}, nil
	}
	return &state, nil
}

func (s *Store) SaveAgentRuntime(ctx context.Context, state *agent.RuntimeState) error {
	if state == nil {
		state = &agent.RuntimeState{}
	}
	return s.saveRuntimeDocument(ctx, "agent-runtime", state)
}

func (s *Store) LoadAssignmentRuntime(ctx context.Context) ([]agent.AssignmentSnapshot, error) {
	var snapshots []agent.AssignmentSnapshot
	ok, err := s.loadRuntimeDocument(ctx, "assignment-runtime", &snapshots)
	if err != nil {
		return nil, err
	}
	if !ok {
		return []agent.AssignmentSnapshot{}, nil
	}
	return snapshots, nil
}

func (s *Store) SaveAssignmentRuntime(ctx context.Context, snapshots []agent.AssignmentSnapshot) error {
	if snapshots == nil {
		snapshots = []agent.AssignmentSnapshot{}
	}
	return s.saveRuntimeDocument(ctx, "assignment-runtime", snapshots)
}

func (s *Store) LoadExecutionRuntime(ctx context.Context) ([]execution.PersistedExecution, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT execution_id, record, total, completed FROM executions ORDER BY started_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []struct {
		id        string
		record    []byte
		total     int
		completed int
	}
	for rows.Next() {
		var d struct {
			id        string
			record    []byte
			total     int
			completed int
		}
		if err := rows.Scan(&d.id, &d.record, &d.total, &d.completed); err != nil {
			return nil, err
		}
		docs = append(docs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return s.migrateFromBlobStore(ctx)
	}

	out := make([]execution.PersistedExecution, 0, len(docs))
	for _, d := range docs {
		var rec execution.ExecutionRecord
		if err := json.Unmarshal(d.record, &rec); err != nil {
			return nil, err
		}
		logs, _ := s.loadExecutionLogs(ctx, d.id)
		out = append(out, execution.PersistedExecution{
			Record:    rec,
			Total:     d.total,
			Completed: d.completed,
			Logs:      logs,
		})
	}
	return out, nil
}

func (s *Store) SaveExecutionRuntime(ctx context.Context, persisted []execution.PersistedExecution) error {
	if len(persisted) == 0 {
		return nil
	}
	for _, item := range persisted {
		if item.Record.ID == "" {
			continue
		}
		record, err := json.Marshal(item.Record)
		if err != nil {
			return err
		}
		startedAt := item.Record.StartedAt
		_, err = s.pool.Exec(ctx, `
INSERT INTO executions (execution_id, started_at, record, total, completed)
VALUES ($1, $2, $3::jsonb, $4, $5)
ON CONFLICT (execution_id) DO UPDATE SET
  record    = EXCLUDED.record,
  total     = EXCLUDED.total,
  completed = EXCLUDED.completed
`, item.Record.ID, startedAt, string(record), item.Total, item.Completed)
		if err != nil {
			return err
		}
		if err := s.saveExecutionLogs(ctx, item.Record.ID, item.Logs); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) loadExecutionLogs(ctx context.Context, executionID string) ([]logstream.Line, error) {
	var payload string
	err := s.pool.QueryRow(ctx,
		`SELECT payload FROM execution_logs WHERE execution_id = $1`, executionID,
	).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) || payload == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lines []logstream.Line
	if err := json.Unmarshal([]byte(payload), &lines); err != nil {
		return nil, err
	}
	return lines, nil
}

func (s *Store) saveExecutionLogs(ctx context.Context, executionID string, lines []logstream.Line) error {
	payload, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO execution_logs (execution_id, payload)
VALUES ($1, $2)
ON CONFLICT (execution_id) DO UPDATE SET payload = EXCLUDED.payload
`, executionID, string(payload))
	return err
}

func (s *Store) migrateFromBlobStore(ctx context.Context) ([]execution.PersistedExecution, error) {
	var persisted []execution.PersistedExecution
	ok, err := s.loadRuntimeDocument(ctx, "execution-runtime", &persisted)
	if err != nil || !ok || len(persisted) == 0 {
		return []execution.PersistedExecution{}, err
	}
	if saveErr := s.SaveExecutionRuntime(ctx, persisted); saveErr == nil {
		s.pool.Exec(ctx, `DELETE FROM runtime_documents WHERE key = 'execution-runtime'`)
	}
	return persisted, nil
}

func (s *Store) loadRuntimeDocument(ctx context.Context, key string, target any) (bool, error) {
	var payload []byte
	err := s.pool.QueryRow(ctx, `SELECT payload FROM runtime_documents WHERE key = $1`, key).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(payload) == 0 {
		return true, nil
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) saveRuntimeDocument(ctx context.Context, key string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO runtime_documents (key, payload, updated_at)
VALUES ($1, $2::jsonb, now())
ON CONFLICT (key) DO UPDATE SET payload = EXCLUDED.payload, updated_at = EXCLUDED.updated_at
`, key, string(payload))
	return err
}

func (s *Store) WriteAuditLog(ctx context.Context, entry *domain.AuditEntry) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO audit_log
  (request_id, method, path, route, status, duration_ms, remote_addr, user_id, workspace_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`,
		entry.RequestID, entry.Method, entry.Path, entry.Route,
		entry.Status, entry.DurationMs, entry.RemoteAddr,
		entry.UserID, entry.WorkspaceID, entry.CreatedAt,
	)
	return err
}

func (s *Store) ListCronJobs(ctx context.Context) ([]cronjobs.CronJob, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, schedule, enabled, email, slack, last_run_at, next_run_at, last_error, created_at, updated_at FROM cron_jobs ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []cronjobs.CronJob
	for rows.Next() {
		var j cronjobs.CronJob
		var emailJSON, slackJSON []byte
		if err := rows.Scan(&j.ID, &j.Name, &j.Schedule, &j.Enabled, &emailJSON, &slackJSON, &j.LastRunAt, &j.NextRunAt, &j.LastError, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(emailJSON, &j.Email)
		_ = json.Unmarshal(slackJSON, &j.Slack)
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Store) GetCronJob(ctx context.Context, id string) (*cronjobs.CronJob, error) {
	var j cronjobs.CronJob
	var emailJSON, slackJSON []byte
	err := s.pool.QueryRow(ctx, `SELECT id, name, schedule, enabled, email, slack, last_run_at, next_run_at, last_error, created_at, updated_at FROM cron_jobs WHERE id = $1`, id).
		Scan(&j.ID, &j.Name, &j.Schedule, &j.Enabled, &emailJSON, &slackJSON, &j.LastRunAt, &j.NextRunAt, &j.LastError, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, cronjobs.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(emailJSON, &j.Email)
	_ = json.Unmarshal(slackJSON, &j.Slack)
	return &j, nil
}

func (s *Store) CreateCronJob(ctx context.Context, job *cronjobs.CronJob) error {
	emailJSON, _ := json.Marshal(job.Email)
	slackJSON, _ := json.Marshal(job.Slack)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO cron_jobs (id, name, schedule, enabled, email, slack, last_error, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		job.ID, job.Name, job.Schedule, job.Enabled, emailJSON, slackJSON, job.LastError, job.CreatedAt, job.UpdatedAt,
	)
	return err
}

func (s *Store) UpdateCronJob(ctx context.Context, job *cronjobs.CronJob) error {
	emailJSON, _ := json.Marshal(job.Email)
	slackJSON, _ := json.Marshal(job.Slack)
	res, err := s.pool.Exec(ctx,
		`UPDATE cron_jobs SET name=$1, schedule=$2, enabled=$3, email=$4, slack=$5, last_run_at=$6, next_run_at=$7, last_error=$8, updated_at=$9 WHERE id=$10`,
		job.Name, job.Schedule, job.Enabled, emailJSON, slackJSON, job.LastRunAt, job.NextRunAt, job.LastError, job.UpdatedAt, job.ID,
	)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return cronjobs.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteCronJob(ctx context.Context, id string) error {
	res, err := s.pool.Exec(ctx, `DELETE FROM cron_jobs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return cronjobs.ErrNotFound
	}
	return nil
}
