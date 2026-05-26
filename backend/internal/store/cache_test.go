package store

import (
	"context"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/cachehub"
	"github.com/babelsuite/babelsuite/internal/domain"
)

// stubStore is a no-op Store implementation used only in tests.
type stubStore struct {
	workspace *domain.Workspace
	user      *domain.User
	err       error
}

func (s *stubStore) CreateWorkspace(_ context.Context, w *domain.Workspace) error {
	s.workspace = w
	return s.err
}
func (s *stubStore) DeleteWorkspace(_ context.Context, _ string) error { return s.err }
func (s *stubStore) GetWorkspaceByID(_ context.Context, _ string) (*domain.Workspace, error) {
	return s.workspace, s.err
}
func (s *stubStore) GetWorkspaceBySlug(_ context.Context, _ string) (*domain.Workspace, error) {
	return s.workspace, s.err
}
func (s *stubStore) CreateUser(_ context.Context, u *domain.User) error {
	s.user = u
	return s.err
}
func (s *stubStore) GetUserByID(_ context.Context, _ string) (*domain.User, error) {
	return s.user, s.err
}
func (s *stubStore) GetUserByEmail(_ context.Context, _ string) (*domain.User, error) {
	return s.user, s.err
}
func (s *stubStore) GetUserByUsername(_ context.Context, _ string) (*domain.User, error) {
	return s.user, s.err
}
func (s *stubStore) StorePasswordResetToken(_ context.Context, _, _ string, _ time.Time) error {
	return s.err
}
func (s *stubStore) ConsumePasswordResetToken(_ context.Context, _, _ string) error { return s.err }
func (s *stubStore) ListFavoritePackageIDs(_ context.Context, _ string) ([]string, error) {
	return nil, s.err
}
func (s *stubStore) SaveFavoritePackage(_ context.Context, _ *domain.FavoritePackage) error {
	return s.err
}
func (s *stubStore) RemoveFavoritePackage(_ context.Context, _, _ string) error { return s.err }
func (s *stubStore) WriteAuditLog(_ context.Context, _ *domain.AuditEntry) error { return s.err }
func (s *stubStore) Close(_ context.Context) error                                { return s.err }

func disabledHub(t *testing.T) *cachehub.Hub {
	t.Helper()
	hub, err := cachehub.New(cachehub.Options{}) // empty address → disabled
	if err != nil {
		t.Fatal(err)
	}
	return hub
}

func TestWithRedisNilBaseReturnsNil(t *testing.T) {
	t.Parallel()
	hub := disabledHub(t)
	result := WithRedis(nil, hub, CacheConfig{})
	if result != nil {
		t.Fatal("expected nil when base is nil")
	}
}

func TestWithRedisNilHubReturnsBase(t *testing.T) {
	t.Parallel()
	base := &stubStore{}
	result := WithRedis(base, nil, CacheConfig{})
	if result != base {
		t.Fatal("expected base store when hub is nil")
	}
}

func TestWithRedisDisabledHubReturnsBase(t *testing.T) {
	t.Parallel()
	base := &stubStore{}
	hub := disabledHub(t)
	result := WithRedis(base, hub, CacheConfig{})
	if result != base {
		t.Fatal("expected base store when hub is disabled")
	}
}

func TestWithRedisDisabledHubPassthrough(t *testing.T) {
	t.Parallel()
	base := &stubStore{workspace: &domain.Workspace{WorkspaceID: "ws1", Slug: "slug1"}}
	hub := disabledHub(t)
	s := WithRedis(base, hub, CacheConfig{})

	ws, err := s.GetWorkspaceByID(context.Background(), "ws1")
	if err != nil {
		t.Fatal(err)
	}
	if ws.WorkspaceID != "ws1" {
		t.Fatalf("expected ws1, got %s", ws.WorkspaceID)
	}
}
