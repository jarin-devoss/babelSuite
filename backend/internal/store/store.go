package store

import (
	"context"
	"errors"
	"time"

	"github.com/babelsuite/babelsuite/internal/domain"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("already exists")
)

type Store interface {
	CreateWorkspace(ctx context.Context, workspace *domain.Workspace) error
	DeleteWorkspace(ctx context.Context, id string) error
	GetWorkspaceByID(ctx context.Context, id string) (*domain.Workspace, error)
	GetWorkspaceBySlug(ctx context.Context, slug string) (*domain.Workspace, error)

	CreateUser(ctx context.Context, user *domain.User) error
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	GetUserByEmail(ctx context.Context, email string) (*domain.User, error)
	GetUserByUsername(ctx context.Context, username string) (*domain.User, error)
	StorePasswordResetToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error
	ConsumePasswordResetToken(ctx context.Context, tokenHash, newPassHash string) error
	ListFavoritePackageIDs(ctx context.Context, userID string) ([]string, error)
	SaveFavoritePackage(ctx context.Context, favorite *domain.FavoritePackage) error
	RemoveFavoritePackage(ctx context.Context, userID, packageID string) error

	WriteAuditLog(ctx context.Context, entry *domain.AuditEntry) error

	Close(ctx context.Context) error
}
