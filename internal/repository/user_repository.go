// Package repository contains persistence implementations backed by storage.
package repository

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	sqlitecloud "github.com/sqlitecloud/sqlitecloud-go"

	"github.com/farigab/bragdev-go/internal/domain"
)

// loginPattern mirrors GitHub's login constraints: alphanumeric and hyphens,
// 1–39 characters, no leading/trailing hyphen.
var loginPattern = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,37}[a-zA-Z0-9])?$|^[a-zA-Z0-9]$`)

// UserRepository defines persistence for users.
type UserRepository interface {
	FindByLogin(ctx context.Context, login string) (*domain.User, error)
	Save(ctx context.Context, u *domain.User) (*domain.User, error)
	ExistsByLogin(ctx context.Context, login string) (bool, error)
	// UpdateGitHubToken updates only the github_access_token column, leaving
	// all other fields (name, avatar_url) untouched.
	UpdateGitHubToken(ctx context.Context, login, token string) error
	ClearGitHubToken(ctx context.Context, login string) error
}

// SQLiteCloudUserRepo implements UserRepository using SQLite Cloud.
type SQLiteCloudUserRepo struct {
	db *sqlitecloud.SQCloud
}

// NewUserRepo creates a new SQLiteCloudUserRepo backed by the given connection.
func NewUserRepo(db *sqlitecloud.SQCloud) *SQLiteCloudUserRepo {
	return &SQLiteCloudUserRepo{db: db}
}

// NewPostgresUserRepo is an alias kept for backward compatibility.
func NewPostgresUserRepo(db *sqlitecloud.SQCloud) *SQLiteCloudUserRepo {
	return NewUserRepo(db)
}

// sqlEscape replaces single quotes with two single quotes.
// NOTE: the sqlitecloud-go driver does not expose a prepared-statement API at
// this time. This escaping is therefore the safest available option. All login
// values are additionally validated against loginPattern before reaching SQL.
func sqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// validateLogin checks the login against the expected GitHub login format
// to prevent unexpected characters from reaching the query.
func validateLogin(login string) error {
	if login == "" {
		return fmt.Errorf("login must not be empty")
	}
	if !loginPattern.MatchString(login) {
		return fmt.Errorf("login contains invalid characters: %q", login)
	}
	return nil
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
func (r *SQLiteCloudUserRepo) FindByLogin(ctx context.Context, login string) (*domain.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateLogin(login); err != nil {
		return nil, err
	}
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

// Save inserts or updates a user record.
// When github_access_token is empty in the incoming record the existing token
// is preserved (no accidental token wipe on profile refresh).
func (r *SQLiteCloudUserRepo) Save(ctx context.Context, u *domain.User) (*domain.User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	if err := validateLogin(u.Login); err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`INSERT INTO users (login, name, avatar_url, github_access_token)
VALUES ('%s', '%s', '%s', '%s')
ON CONFLICT(login) DO UPDATE SET
  name                = excluded.name,
  avatar_url          = excluded.avatar_url,
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
		return r.FindByLogin(ctx, u.Login)
	}
	return scanUser(result, 0), nil
}

// ExistsByLogin returns true when a user with the given login exists.
func (r *SQLiteCloudUserRepo) ExistsByLogin(ctx context.Context, login string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := validateLogin(login); err != nil {
		return false, err
	}
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

// UpdateGitHubToken sets only the github_access_token for the given user without
// touching name or avatar_url. Prefer this over Save when only the token changes
// — using Save with an incomplete User struct would silently wipe those fields.
func (r *SQLiteCloudUserRepo) UpdateGitHubToken(ctx context.Context, login, token string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateLogin(login); err != nil {
		return err
	}
	q := fmt.Sprintf(
		"UPDATE users SET github_access_token = '%s' WHERE login = '%s';",
		sqlEscape(token),
		sqlEscape(login),
	)
	return r.db.Execute(q)
}

// ClearGitHubToken sets the stored GitHub access token to empty for the given user.
func (r *SQLiteCloudUserRepo) ClearGitHubToken(ctx context.Context, login string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateLogin(login); err != nil {
		return err
	}
	q := fmt.Sprintf(
		"UPDATE users SET github_access_token = '' WHERE login = '%s';",
		sqlEscape(login),
	)
	return r.db.Execute(q)
}
