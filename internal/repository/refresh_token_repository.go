// Package repository contains repository implementations used by the application.
package repository

import (
	"context"
	"fmt"

	"bragdev-go/internal/db"
	"bragdev-go/internal/domain"
)

// RefreshTokenRepository defines operations for refresh tokens.
type RefreshTokenRepository interface {
	Save(ctx context.Context, t *domain.RefreshToken) (*domain.RefreshToken, error)
	FindByToken(ctx context.Context, token string) (*domain.RefreshToken, error)
	FindByUserLogin(ctx context.Context, userLogin string) ([]*domain.RefreshToken, error)
	Delete(ctx context.Context, t *domain.RefreshToken) error
	DeleteAllByUserLogin(ctx context.Context, userLogin string) error
	DeleteExpiredTokens(ctx context.Context) error
}

const rtCols = "token, user_login, expires_at, created_at, revoked"

type refreshTokenRepo struct{ db db.DB }

// NewRefreshTokenRepo constructs a RefreshTokenRepository using the provided DB.
func NewRefreshTokenRepo(d db.DB) RefreshTokenRepository { return &refreshTokenRepo{db: d} }

// NewPostgresRefreshTokenRepo kept as alias for compatibility.
func NewPostgresRefreshTokenRepo(d db.DB) RefreshTokenRepository { return NewRefreshTokenRepo(d) }

func scanRT(rows *db.Rows, row int) *domain.RefreshToken {
	return &domain.RefreshToken{
		Token:     rows.String(row, 0),
		UserLogin: rows.String(row, 1),
		ExpiresAt: rows.Time(row, 2),
		CreatedAt: rows.Time(row, 3),
		Revoked:   rows.Bool(row, 4),
	}
}

func (r *refreshTokenRepo) Save(ctx context.Context, t *domain.RefreshToken) (*domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	rows, err := r.db.Query(ctx,
		db.Insert("refresh_tokens").
			Columns("token", "user_login", "expires_at", "created_at", "revoked").
			Values(db.String(t.Token), db.String(t.UserLogin), db.Time(t.ExpiresAt), db.Time(t.CreatedAt), db.Bool(t.Revoked)).
			OnConflict("token").
			DoUpdate(db.SetExcluded("user_login"), db.SetExcluded("expires_at"), db.SetExcluded("created_at"), db.SetExcluded("revoked")).
			Returning(rtCols),
	)
	if err != nil {
		return nil, err
	}
	if rows == nil || rows.IsEmpty() {
		return r.FindByToken(ctx, t.Token)
	}
	return scanRT(rows, 0), nil
}

func (r *refreshTokenRepo) FindByToken(ctx context.Context, token string) (*domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx,
		db.Select(rtCols).From("refresh_tokens").Where(db.Eq("token", db.String(token))),
	)
	if err != nil {
		return nil, err
	}
	if rows == nil || rows.IsEmpty() {
		return nil, fmt.Errorf("refresh token not found")
	}
	return scanRT(rows, 0), nil
}

func (r *refreshTokenRepo) FindByUserLogin(ctx context.Context, userLogin string) ([]*domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx,
		db.Select(rtCols).From("refresh_tokens").Where(db.Eq("user_login", db.String(userLogin))),
	)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, nil
	}
	list := make([]*domain.RefreshToken, rows.Len())
	for i := range list {
		list[i] = scanRT(rows, i)
	}
	return list, nil
}

func (r *refreshTokenRepo) Delete(ctx context.Context, t *domain.RefreshToken) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t == nil {
		return nil
	}
	return r.db.Exec(ctx, db.Delete("refresh_tokens").Where(db.Eq("token", db.String(t.Token))))
}

func (r *refreshTokenRepo) DeleteAllByUserLogin(ctx context.Context, userLogin string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.db.Exec(ctx, db.Delete("refresh_tokens").Where(db.Eq("user_login", db.String(userLogin))))
}

func (r *refreshTokenRepo) DeleteExpiredTokens(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.db.Exec(ctx, db.Delete("refresh_tokens").Where(db.RawCond("expires_at < datetime('now')")))
}
