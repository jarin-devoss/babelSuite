// Package storetest is the contract every datastore must meet: the same calls
// on MongoDB and on PostgreSQL give the same answers for what the server relies
// on. A store is run through it by one test per driver (contract_test.go), the way
// a datastore is tested on whichever driver the environment names.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/agent"
	"github.com/babelsuite/babelsuite/internal/cronjobs"
	"github.com/babelsuite/babelsuite/internal/domain"
	"github.com/babelsuite/babelsuite/internal/execution"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/store"
)

// Run checks a store against the contract. newStore returns a store on an empty
// database; the store is closed when the test ends.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()
	for name, check := range map[string]func(*testing.T, store.Store){
		"workspaces":            workspaces,
		"users":                 users,
		"password reset tokens": passwordResetTokens,
		"favorites":             favorites,
		"audit log":             auditLog,
		"agent runtime":         agentRuntime,
		"assignment runtime":    assignmentRuntime,
		"execution runtime":     executionRuntime,
		"cron jobs":             cronJobs,
	} {
		check := check
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			t.Cleanup(func() { _ = s.Close(context.Background()) })
			check(t, s)
		})
	}
}

func ctx() context.Context { return context.Background() }

func expect(t *testing.T, err, want error, what string) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("%s: err = %v, want %v", what, err, want)
	}
}

func noError(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func workspaces(t *testing.T, s store.Store) {
	workspace := &domain.Workspace{WorkspaceID: "w1", Slug: "acme", Name: "Acme", CreatedAt: now()}
	noError(t, s.CreateWorkspace(ctx(), workspace), "create")

	got, err := s.GetWorkspaceByID(ctx(), "w1")
	noError(t, err, "get by id")
	if got.Slug != "acme" || got.Name != "Acme" {
		t.Fatalf("get by id = %+v", got)
	}
	got, err = s.GetWorkspaceBySlug(ctx(), "acme")
	noError(t, err, "get by slug")
	if got.WorkspaceID != "w1" {
		t.Fatalf("get by slug = %+v", got)
	}

	_, err = s.GetWorkspaceByID(ctx(), "missing")
	expect(t, err, store.ErrNotFound, "an unknown workspace id")
	_, err = s.GetWorkspaceBySlug(ctx(), "missing")
	expect(t, err, store.ErrNotFound, "an unknown workspace slug")

	expect(t, s.CreateWorkspace(ctx(), &domain.Workspace{WorkspaceID: "w2", Slug: "acme", Name: "Other", CreatedAt: now()}), store.ErrDuplicate, "a duplicate slug")
	expect(t, s.CreateWorkspace(ctx(), &domain.Workspace{WorkspaceID: "w1", Slug: "other", Name: "Other", CreatedAt: now()}), store.ErrDuplicate, "a duplicate id")

	noError(t, s.DeleteWorkspace(ctx(), "w1"), "delete")
	_, err = s.GetWorkspaceByID(ctx(), "w1")
	expect(t, err, store.ErrNotFound, "a deleted workspace")
}

func newUser(id, email, username string) *domain.User {
	return &domain.User{UserID: id, WorkspaceID: "w1", Username: username, Email: email, FullName: "Ada Lovelace", IsAdmin: true, PassHash: "hash-1", CreatedAt: now()}
}

func users(t *testing.T, s store.Store) {
	noError(t, s.CreateWorkspace(ctx(), &domain.Workspace{WorkspaceID: "w1", Slug: "acme", Name: "Acme", CreatedAt: now()}), "workspace")
	noError(t, s.CreateUser(ctx(), newUser("u1", "ada@example.com", "ada")), "create")

	for name, get := range map[string]func() (*domain.User, error){
		"id":       func() (*domain.User, error) { return s.GetUserByID(ctx(), "u1") },
		"email":    func() (*domain.User, error) { return s.GetUserByEmail(ctx(), "ada@example.com") },
		"username": func() (*domain.User, error) { return s.GetUserByUsername(ctx(), "ada") },
	} {
		user, err := get()
		noError(t, err, "get by "+name)
		if user.UserID != "u1" || user.Email != "ada@example.com" || user.Username != "ada" || !user.IsAdmin || user.PassHash != "hash-1" || user.FullName != "Ada Lovelace" || user.WorkspaceID != "w1" {
			t.Fatalf("get by %s = %+v", name, user)
		}
	}

	_, err := s.GetUserByID(ctx(), "missing")
	expect(t, err, store.ErrNotFound, "an unknown user id")
	_, err = s.GetUserByEmail(ctx(), "nobody@example.com")
	expect(t, err, store.ErrNotFound, "an unknown email")
	_, err = s.GetUserByUsername(ctx(), "nobody")
	expect(t, err, store.ErrNotFound, "an unknown username")

	expect(t, s.CreateUser(ctx(), newUser("u2", "ada@example.com", "other")), store.ErrDuplicate, "a duplicate email")
	expect(t, s.CreateUser(ctx(), newUser("u3", "other@example.com", "ada")), store.ErrDuplicate, "a duplicate username")
	expect(t, s.CreateUser(ctx(), newUser("u1", "third@example.com", "third")), store.ErrDuplicate, "a duplicate user id")
}

func passwordResetTokens(t *testing.T, s store.Store) {
	noError(t, s.CreateWorkspace(ctx(), &domain.Workspace{WorkspaceID: "w1", Slug: "acme", Name: "Acme", CreatedAt: now()}), "workspace")
	noError(t, s.CreateUser(ctx(), newUser("u1", "ada@example.com", "ada")), "user")

	noError(t, s.StorePasswordResetToken(ctx(), "u1", "token-hash", time.Now().Add(15*time.Minute)), "store")
	noError(t, s.ConsumePasswordResetToken(ctx(), "token-hash", "hash-2"), "consume")
	user, err := s.GetUserByID(ctx(), "u1")
	noError(t, err, "get")
	if user.PassHash != "hash-2" {
		t.Fatalf("the password was not changed: %q", user.PassHash)
	}
	expect(t, s.ConsumePasswordResetToken(ctx(), "token-hash", "hash-3"), store.ErrNotFound, "a token used twice")

	noError(t, s.StorePasswordResetToken(ctx(), "u1", "stale", time.Now().Add(-time.Minute)), "store an expired token")
	expect(t, s.ConsumePasswordResetToken(ctx(), "stale", "hash-4"), store.ErrNotFound, "an expired token")
	expect(t, s.ConsumePasswordResetToken(ctx(), "never-stored", "hash-5"), store.ErrNotFound, "an unknown token")

	// A second request replaces the first: only the latest link works.
	noError(t, s.StorePasswordResetToken(ctx(), "u1", "first", time.Now().Add(time.Hour)), "store first")
	noError(t, s.StorePasswordResetToken(ctx(), "u1", "second", time.Now().Add(time.Hour)), "store second")
	expect(t, s.ConsumePasswordResetToken(ctx(), "first", "hash-6"), store.ErrNotFound, "a replaced token")
	noError(t, s.ConsumePasswordResetToken(ctx(), "second", "hash-7"), "the latest token")
}

func favorites(t *testing.T, s store.Store) {
	noError(t, s.CreateWorkspace(ctx(), &domain.Workspace{WorkspaceID: "w1", Slug: "acme", Name: "Acme", CreatedAt: now()}), "workspace")
	noError(t, s.CreateUser(ctx(), newUser("u1", "ada@example.com", "ada")), "user 1")
	noError(t, s.CreateUser(ctx(), newUser("u2", "grace@example.com", "grace")), "user 2")
	ids, err := s.ListFavoritePackageIDs(ctx(), "u1")
	noError(t, err, "list when empty")
	if len(ids) != 0 {
		t.Fatalf("list when empty = %v", ids)
	}
	noError(t, s.SaveFavoritePackage(ctx(), &domain.FavoritePackage{UserID: "u1", PackageID: "pkg-a", CreatedAt: now()}), "save a")
	noError(t, s.SaveFavoritePackage(ctx(), &domain.FavoritePackage{UserID: "u1", PackageID: "pkg-b", CreatedAt: now()}), "save b")
	// Saving a favourite twice is not an error: starring twice must not fail.
	noError(t, s.SaveFavoritePackage(ctx(), &domain.FavoritePackage{UserID: "u1", PackageID: "pkg-a", CreatedAt: now()}), "save a again")
	noError(t, s.SaveFavoritePackage(ctx(), &domain.FavoritePackage{UserID: "u2", PackageID: "pkg-c", CreatedAt: now()}), "another user's")

	ids, err = s.ListFavoritePackageIDs(ctx(), "u1")
	noError(t, err, "list")
	if len(ids) != 2 || !contains(ids, "pkg-a") || !contains(ids, "pkg-b") {
		t.Fatalf("list = %v, want pkg-a and pkg-b once each", ids)
	}
	noError(t, s.RemoveFavoritePackage(ctx(), "u1", "pkg-a"), "remove")
	noError(t, s.RemoveFavoritePackage(ctx(), "u1", "pkg-a"), "remove again")
	ids, _ = s.ListFavoritePackageIDs(ctx(), "u1")
	if len(ids) != 1 || ids[0] != "pkg-b" {
		t.Fatalf("list after remove = %v", ids)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func auditLog(t *testing.T, s store.Store) {
	noError(t, s.WriteAuditLog(ctx(), &domain.AuditEntry{RequestID: "r1", Method: "GET", Path: "/x", Route: "/x", Status: 200, DurationMs: 3, RemoteAddr: "127.0.0.1", UserID: "u1", WorkspaceID: "w1", CreatedAt: now()}), "write")
}

func agentRuntime(t *testing.T, s store.Store) {
	repository, ok := s.(agent.RuntimeRepository)
	if !ok {
		t.Fatal("the store does not implement agent.RuntimeRepository")
	}
	empty, err := repository.LoadAgentRuntime(ctx())
	noError(t, err, "load when empty")
	if empty != nil && len(empty.Agents) != 0 {
		t.Fatalf("load when empty = %+v", empty)
	}

	state := &agent.RuntimeState{Agents: []agent.Registration{
		{AgentID: "a1", Name: "Agent 1", HostURL: "http://a1:8091", Status: "online", Capabilities: []string{"docker"}, RegisteredAt: now(), LastHeartbeatAt: now(), Capacity: 4},
		{AgentID: "a2", Name: "Agent 2", HostURL: "http://a2:8091", Status: "offline", RegisteredAt: now(), LastHeartbeatAt: now()},
	}, UpdatedAt: now()}
	noError(t, repository.SaveAgentRuntime(ctx(), state), "save")
	got, err := repository.LoadAgentRuntime(ctx())
	noError(t, err, "load")
	if len(got.Agents) != 2 || got.Agents[0].AgentID != "a1" || got.Agents[0].Capacity != 4 || got.Agents[1].Status != "offline" {
		t.Fatalf("load = %+v", got)
	}

	// Saving replaces what was there.
	state.Agents = state.Agents[:1]
	noError(t, repository.SaveAgentRuntime(ctx(), state), "save fewer")
	got, _ = repository.LoadAgentRuntime(ctx())
	if len(got.Agents) != 1 {
		t.Fatalf("a removed agent came back: %+v", got.Agents)
	}
}

func assignmentRuntime(t *testing.T, s store.Store) {
	repository, ok := s.(agent.AssignmentStore)
	if !ok {
		t.Fatal("the store does not implement agent.AssignmentStore")
	}
	empty, err := repository.LoadAssignmentRuntime(ctx())
	noError(t, err, "load when empty")
	if len(empty) != 0 {
		t.Fatalf("load when empty = %+v", empty)
	}
	snapshots := []agent.AssignmentSnapshot{
		{Request: agent.StepRequest{JobID: "j1", ExecutionID: "e1", SuiteID: "s", Env: map[string]string{"K": "v"}}, Status: agent.AssignmentSucceeded, ClaimedBy: "a1", Completed: true},
		{Request: agent.StepRequest{JobID: "j2", ExecutionID: "e1", SuiteID: "s"}, Status: agent.AssignmentFailed, Error: "boom", CancelRequested: true},
	}
	noError(t, repository.SaveAssignmentRuntime(ctx(), snapshots), "save")
	got, err := repository.LoadAssignmentRuntime(ctx())
	noError(t, err, "load")
	if len(got) != 2 || got[0].Request.JobID != "j1" || got[0].Request.Env["K"] != "v" || got[0].ClaimedBy != "a1" || got[1].Error != "boom" || !got[1].CancelRequested {
		t.Fatalf("load = %+v", got)
	}
	noError(t, repository.SaveAssignmentRuntime(ctx(), snapshots[:1]), "save fewer")
	got, _ = repository.LoadAssignmentRuntime(ctx())
	if len(got) != 1 {
		t.Fatalf("a removed assignment came back: %+v", got)
	}
}

func executionRuntime(t *testing.T, s store.Store) {
	repository, ok := s.(execution.RuntimeStore)
	if !ok {
		t.Fatal("the store does not implement execution.RuntimeStore")
	}
	empty, err := repository.LoadExecutionRuntime(ctx())
	noError(t, err, "load when empty")
	if len(empty) != 0 {
		t.Fatalf("load when empty = %+v", empty)
	}

	record := func(id, status string) execution.PersistedExecution {
		return execution.PersistedExecution{
			Record: execution.ExecutionRecord{
				ID: id, Profile: "local.yaml", Status: status, StartedAt: now(), UpdatedAt: now(),
				Events: []execution.ExecutionEvent{{ID: "start", Source: "boot", Text: "started", Level: "info"}},
			},
			Total: 3, Completed: 1,
			Logs: []logstream.Line{{Source: "step", Level: "info", Kind: "output", Text: "hello " + id}},
		}
	}
	noError(t, repository.SaveExecutionRuntime(ctx(), []execution.PersistedExecution{record("e1", "Healthy"), record("e2", "Failed")}), "save")
	got, err := repository.LoadExecutionRuntime(ctx())
	noError(t, err, "load")
	if len(got) != 2 {
		t.Fatalf("load = %d executions, want 2", len(got))
	}
	byID := map[string]execution.PersistedExecution{}
	for _, item := range got {
		byID[item.Record.ID] = item
	}
	first := byID["e1"]
	if first.Record.Status != "Healthy" || first.Total != 3 || first.Completed != 1 || len(first.Record.Events) != 1 || len(first.Logs) != 1 || first.Logs[0].Text != "hello e1" {
		t.Fatalf("e1 did not round-trip: %+v", first)
	}
	if byID["e2"].Record.Status != "Failed" {
		t.Fatalf("e2 did not round-trip: %+v", byID["e2"])
	}

	// A save writes the executions it is given: it updates what it names and does
	// not delete what it leaves out, so a restart reloads every run that was ever
	// saved, not only the ones in memory when the last save ran.
	noError(t, repository.SaveExecutionRuntime(ctx(), []execution.PersistedExecution{record("e1", "Failed")}), "save again")
	got, _ = repository.LoadExecutionRuntime(ctx())
	if len(got) != 2 {
		t.Fatalf("after the second save there are %d executions, want 2 (a save does not delete)", len(got))
	}
	for _, item := range got {
		if item.Record.ID == "e1" && item.Record.Status != "Failed" {
			t.Fatalf("e1 was not updated: %+v", item.Record)
		}
	}
}

func cronJobs(t *testing.T, s store.Store) {
	repository, ok := s.(cronjobs.Store)
	if !ok {
		t.Fatal("the store does not implement cronjobs.Store")
	}
	jobs, err := repository.ListCronJobs(ctx())
	noError(t, err, "list when empty")
	if len(jobs) != 0 {
		t.Fatalf("list when empty = %+v", jobs)
	}

	job := &cronjobs.CronJob{
		ID: "c1", Name: "Nightly", Schedule: "0 2 * * *", Enabled: true,
		Suites:    []cronjobs.SuiteTarget{{SuiteID: "payment-suite", Profile: "local.yaml", BackendID: "local-docker"}},
		Email:     cronjobs.EmailConfig{Recipients: []string{"a@example.com"}, Subject: "Report"},
		Slack:     cronjobs.SlackConfig{WebhookURL: "https://hooks.example/x"},
		CreatedAt: now(), UpdatedAt: now(),
	}
	noError(t, repository.CreateCronJob(ctx(), job), "create")

	got, err := repository.GetCronJob(ctx(), "c1")
	noError(t, err, "get")
	if got.Name != "Nightly" || got.Schedule != "0 2 * * *" || !got.Enabled || len(got.Suites) != 1 || got.Suites[0].SuiteID != "payment-suite" || len(got.Email.Recipients) != 1 || got.Slack.WebhookURL == "" {
		t.Fatalf("get = %+v", got)
	}
	_, err = repository.GetCronJob(ctx(), "missing")
	expect(t, err, cronjobs.ErrNotFound, "an unknown cron job")

	last := now()
	job.Name = "Renamed"
	job.Enabled = false
	job.LastRunAt = &last
	job.LastError = "boom"
	noError(t, repository.UpdateCronJob(ctx(), job), "update")
	got, _ = repository.GetCronJob(ctx(), "c1")
	if got.Name != "Renamed" || got.Enabled || got.LastRunAt == nil || got.LastError != "boom" {
		t.Fatalf("after update = %+v", got)
	}
	expect(t, repository.UpdateCronJob(ctx(), &cronjobs.CronJob{ID: "missing"}), cronjobs.ErrNotFound, "updating an unknown cron job")

	jobs, _ = repository.ListCronJobs(ctx())
	if len(jobs) != 1 {
		t.Fatalf("list = %+v", jobs)
	}
	noError(t, repository.DeleteCronJob(ctx(), "c1"), "delete")
	expect(t, repository.DeleteCronJob(ctx(), "c1"), cronjobs.ErrNotFound, "deleting an unknown cron job")
}
