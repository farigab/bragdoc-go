// Package repository contains persistence implementations backed by storage.
package repository

import (
	"fmt"
	"strings"

	sqlitecloud "github.com/sqlitecloud/sqlitecloud-go"

	"github.com/farigab/bragdev-go/internal/domain"
)

// UserRepository defines persistence for users.
type UserRepository interface {
	FindByLogin(login string) (*domain.User, error)
	Save(u *domain.User) (*domain.User, error)
	ExistsByLogin(login string) (bool, error)
	ClearGitHubToken(login string) error
}

// SQLiteCloudUserRepo implements UserRepository using SQLite Cloud.
type SQLiteCloudUserRepo struct {
	db *sqlitecloud.SQCloud
}

// NewUserRepo creates a new SQLiteCloudUserRepo backed by the given connection.
// The name NewPostgresUserRepo is kept as an alias for backward compatibility.
func NewUserRepo(db *sqlitecloud.SQCloud) *SQLiteCloudUserRepo {
	return &SQLiteCloudUserRepo{db: db}
}

// NewPostgresUserRepo is an alias for NewUserRepo kept for compatibility.
func NewPostgresUserRepo(db *sqlitecloud.SQCloud) *SQLiteCloudUserRepo {
	return NewUserRepo(db)
}

// sqlEscape replaces single quotes with two single quotes for SQL safety.
// Only use for trusted internal values; prefer parameterized queries when available.
func sqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// scanUser reads a user from result at the given row index.
func scanUser(result *sqlitecloud.Result, row uint64) *domain.User {
	login, _ := result.GetStringValue(row, 0)
	name, _ := result.GetStringValue(row, 1)
	avatar, _ := result.GetStringValue(row, 2)
	token, _ := result.GetStringValue(row, 3)
	return &domain.User{
		Login:             login,
		Name:              name,
		AvatarURL:         avatar,
		GitHubAccessToken: token,
	}
}

// FindByLogin looks up a user by login.
func (r *SQLiteCloudUserRepo) FindByLogin(login string) (*domain.User, error) {
	q := fmt.Sprintf(
		"SELECT login, name, avatar_url, github_access_token FROM users WHERE login='%s';",
		sqlEscape(login),
	)
	result, err := r.db.Select(q)
	if err != nil {
		return nil, err
	}
	if result == nil || result.GetNumberOfRows() == 0 {
		return nil, fmt.Errorf("user not found: %s", login)
	}
	return scanUser(result, 0), nil
}

// Save inserts or updates a user record and returns the stored entity.
// If github_access_token is empty in the incoming record the existing token is preserved.
func (r *SQLiteCloudUserRepo) Save(u *domain.User) (*domain.User, error) {
	if u == nil {
		return nil, nil
	}
	q := fmt.Sprintf(`INSERT INTO users (login, name, avatar_url, github_access_token)
VALUES ('%s', '%s', '%s', '%s')
ON CONFLICT(login) DO UPDATE SET
  name               = excluded.name,
  avatar_url         = excluded.avatar_url,
  github_access_token = CASE
    WHEN excluded.github_access_token <> '' THEN excluded.github_access_token
    ELSE users.github_access_token
  END
RETURNING login, name, avatar_url, github_access_token;`,
		sqlEscape(u.Login),
		sqlEscape(u.Name),
		sqlEscape(u.AvatarURL),
		sqlEscape(u.GitHubAccessToken),
	)
	result, err := r.db.Select(q)
	if err != nil {
		return nil, err
	}
	// Fallback: RETURNING may not be available in all SQLite Cloud configurations.
	if result == nil || result.GetNumberOfRows() == 0 {
		return r.FindByLogin(u.Login)
	}
	return scanUser(result, 0), nil
}

// ExistsByLogin returns true when a user with the given login exists.
func (r *SQLiteCloudUserRepo) ExistsByLogin(login string) (bool, error) {
	q := fmt.Sprintf(
		"SELECT COUNT(*) FROM users WHERE login='%s';",
		sqlEscape(login),
	)
	result, err := r.db.Select(q)
	if err != nil {
		return false, err
	}
	if result == nil || result.GetNumberOfRows() == 0 {
		return false, nil
	}
	count, err := result.GetInt64Value(0, 0)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ClearGitHubToken clears the stored GitHub access token for the given user.
func (r *SQLiteCloudUserRepo) ClearGitHubToken(login string) error {
	q := fmt.Sprintf(
		"UPDATE users SET github_access_token = '' WHERE login = '%s';",
		sqlEscape(login),
	)
	return r.db.Execute(q)
}
