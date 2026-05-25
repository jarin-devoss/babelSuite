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
  suites      JSONB NOT NULL DEFAULT '[]',
  email       JSONB NOT NULL DEFAULT '{}',
  slack       JSONB NOT NULL DEFAULT '{}',
  last_run_at TIMESTAMPTZ,
  next_run_at TIMESTAMPTZ,
  last_error  TEXT NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
