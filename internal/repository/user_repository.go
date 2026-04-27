// Package repository contains repository implementations used by the application.
package repository

import (
	"context"
	"fmt"
	"regexp"

	"github.com/farigab/bragdev-go/internal/db"
	"github.com/farigab/bragdev-go/internal/domain"
)

var loginPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,37}[a-zA-Z0-9])?$|^[a-zA-Z0-9]$`)

// UserRepository defines operations for working with users.
type UserRepository interface {
	FindByLogin(ctx context.Context, login string) (*domain.User, error)
	Save(ctx context.Context, u *domain.User) (*domain.User, error)
	ExistsByLogin(ctx context.Context, login string) (bool, error)
	UpdateGitHubToken(ctx context.Context, login, token string) error
	ClearGitHubToken(ctx context.Context, login string) error
}

type userRepo struct{ db db.DB }

// NewUserRepo constructs a UserRepository using the provided DB.
func NewUserRepo(d db.DB) UserRepository { return &userRepo{db: d} }

const userCols = "login, name, avatar_url, github_access_token"

func validateLogin(login string) error {
	if login == "" {
		return fmt.Errorf("login must not be empty")
	}
	if !loginPattern.MatchString(login) {
		return fmt.Errorf("login contains invalid characters: %q", login)
	}
	return nil
}

func scanUser(rows *db.Rows, row int) *domain.User {
	return &domain.User{
		Login:             rows.String(row, 0),
		Name:              rows.String(row, 1),
		AvatarURL:         rows.String(row, 2),
		GitHubAccessToken: rows.String(row, 3),
	}
}

func (r *userRepo) FindByLogin(ctx context.Context, login string) (*domain.User, error) {
	if err := validateLogin(login); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx,
		db.Select(userCols).From("users").Where(db.Eq("login", db.String(login))),
	)
	if err != nil {
		return nil, err
	}
	if rows.IsEmpty() {
		return nil, fmt.Errorf("user not found: %s", login)
	}
	return scanUser(rows, 0), nil
}

func (r *userRepo) Save(ctx context.Context, u *domain.User) (*domain.User, error) {
	if u == nil {
		return nil, nil
	}
	if err := validateLogin(u.Login); err != nil {
		return nil, err
	}

	// A regra de negócio do github_access_token (preservar se vazio) fica aqui
	// em SetRaw — é lógica da aplicação, não do builder.
	const tokenCase = "CASE WHEN excluded.github_access_token <> '' " +
		"THEN excluded.github_access_token " +
		"ELSE users.github_access_token END"

	rows, err := r.db.Query(ctx,
		db.Insert("users").
			Columns("login", "name", "avatar_url", "github_access_token").
			Values(db.String(u.Login), db.String(u.Name), db.String(u.AvatarURL), db.String(u.GitHubAccessToken)).
			OnConflict("login").
			DoUpdate(
				db.SetExcluded("name"),
				db.SetExcluded("avatar_url"),
				db.SetRaw("github_access_token", tokenCase),
			).
			Returning(userCols),
	)
	if err != nil {
		return nil, err
	}
	if rows.IsEmpty() {
		// Fallback: RETURNING pode não estar disponível em todas as configs do SQLite Cloud.
		return r.FindByLogin(ctx, u.Login)
	}
	return scanUser(rows, 0), nil
}

func (r *userRepo) ExistsByLogin(ctx context.Context, login string) (bool, error) {
	if err := validateLogin(login); err != nil {
		return false, err
	}
	rows, err := r.db.Query(ctx,
		db.Select("COUNT(*)").From("users").Where(db.Eq("login", db.String(login))),
	)
	if err != nil {
		return false, err
	}
	return rows.Int64(0, 0) > 0, nil
}

func (r *userRepo) UpdateGitHubToken(ctx context.Context, login, token string) error {
	if err := validateLogin(login); err != nil {
		return err
	}
	return r.db.Exec(ctx,
		db.Update("users").
			Set("github_access_token", db.String(token)).
			Where(db.Eq("login", db.String(login))),
	)
}

func (r *userRepo) ClearGitHubToken(ctx context.Context, login string) error {
	if err := validateLogin(login); err != nil {
		return err
	}
	return r.db.Exec(ctx,
		db.Update("users").
			Set("github_access_token", db.String("")).
			Where(db.Eq("login", db.String(login))),
	)
}
