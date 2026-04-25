package repository

import (
	"fmt"
	"time"

	sqlitecloud "github.com/sqlitecloud/sqlitecloud-go"

	"github.com/farigab/bragdev-go/internal/domain"
)

// RefreshTokenRepository defines operations for refresh tokens.
type RefreshTokenRepository interface {
	Save(t *domain.RefreshToken) (*domain.RefreshToken, error)
	FindByToken(token string) (*domain.RefreshToken, error)
	FindByUserLogin(userLogin string) ([]*domain.RefreshToken, error)
	Delete(t *domain.RefreshToken) error
	DeleteAllByUserLogin(userLogin string) error
	DeleteExpiredTokens() error
}

// SQLiteCloudRefreshTokenRepo implements RefreshTokenRepository using SQLite Cloud.
type SQLiteCloudRefreshTokenRepo struct {
	db *sqlitecloud.SQCloud
}

// NewRefreshTokenRepo creates a new SQLiteCloudRefreshTokenRepo.
// The name NewPostgresRefreshTokenRepo is kept as an alias for backward compatibility.
func NewRefreshTokenRepo(db *sqlitecloud.SQCloud) *SQLiteCloudRefreshTokenRepo {
	return &SQLiteCloudRefreshTokenRepo{db: db}
}

// NewPostgresRefreshTokenRepo is an alias for NewRefreshTokenRepo kept for compatibility.
func NewPostgresRefreshTokenRepo(db *sqlitecloud.SQCloud) *SQLiteCloudRefreshTokenRepo {
	return NewRefreshTokenRepo(db)
}

// timeLayout is used for storing and parsing time values in SQLite (TEXT columns).
const timeLayout = time.RFC3339

// scanRefreshToken reads a RefreshToken from result at the given row index.
// Column order: token(0), user_login(1), expires_at(2), created_at(3), revoked(4).
func scanRefreshToken(result *sqlitecloud.Result, row uint64) (*domain.RefreshToken, error) {
	token, _ := result.GetStringValue(row, 0)
	userLogin, _ := result.GetStringValue(row, 1)
	expiresAtStr, _ := result.GetStringValue(row, 2)
	createdAtStr, _ := result.GetStringValue(row, 3)
	revokedInt, _ := result.GetInt64Value(row, 4)

	expiresAt, _ := time.Parse(timeLayout, expiresAtStr)
	createdAt, _ := time.Parse(timeLayout, createdAtStr)

	return &domain.RefreshToken{
		Token:     token,
		UserLogin: userLogin,
		ExpiresAt: expiresAt,
		CreatedAt: createdAt,
		Revoked:   revokedInt != 0,
	}, nil
}

// boolToInt converts a Go bool to SQLite INTEGER (0 or 1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Save inserts or updates a refresh token record and returns the persisted entity.
func (r *SQLiteCloudRefreshTokenRepo) Save(t *domain.RefreshToken) (*domain.RefreshToken, error) {
	if t == nil {
		return nil, nil
	}
	q := fmt.Sprintf(`INSERT INTO refresh_tokens (token, user_login, expires_at, created_at, revoked)
VALUES ('%s', '%s', '%s', '%s', %d)
ON CONFLICT(token) DO UPDATE SET
  user_login = excluded.user_login,
  expires_at = excluded.expires_at,
  created_at = excluded.created_at,
  revoked    = excluded.revoked
RETURNING token, user_login, expires_at, created_at, revoked;`,
		sqlEscape(t.Token),
		sqlEscape(t.UserLogin),
		t.ExpiresAt.UTC().Format(timeLayout),
		t.CreatedAt.UTC().Format(timeLayout),
		boolToInt(t.Revoked),
	)
	result, err := r.db.Select(q)
	if err != nil {
		return nil, err
	}
	// Fallback: RETURNING may not be available in all SQLite Cloud configurations.
	if result == nil || result.GetNumberOfRows() == 0 {
		return r.FindByToken(t.Token)
	}
	return scanRefreshToken(result, 0)
}

// FindByToken retrieves a refresh token by its string value.
func (r *SQLiteCloudRefreshTokenRepo) FindByToken(token string) (*domain.RefreshToken, error) {
	q := fmt.Sprintf(
		"SELECT token, user_login, expires_at, created_at, revoked FROM refresh_tokens WHERE token='%s';",
		sqlEscape(token),
	)
	result, err := r.db.Select(q)
	if err != nil {
		return nil, err
	}
	if result == nil || result.GetNumberOfRows() == 0 {
		return nil, fmt.Errorf("refresh token not found")
	}
	return scanRefreshToken(result, 0)
}

// FindByUserLogin returns all refresh tokens for a given user login.
func (r *SQLiteCloudRefreshTokenRepo) FindByUserLogin(userLogin string) ([]*domain.RefreshToken, error) {
	q := fmt.Sprintf(
		"SELECT token, user_login, expires_at, created_at, revoked FROM refresh_tokens WHERE user_login='%s';",
		sqlEscape(userLogin),
	)
	result, err := r.db.Select(q)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	list := make([]*domain.RefreshToken, 0, result.GetNumberOfRows())
	for row := uint64(0); row < result.GetNumberOfRows(); row++ {
		rt, err := scanRefreshToken(result, row)
		if err != nil {
			return nil, err
		}
		list = append(list, rt)
	}
	return list, nil
}

// Delete removes the provided refresh token record.
func (r *SQLiteCloudRefreshTokenRepo) Delete(t *domain.RefreshToken) error {
	if t == nil {
		return nil
	}
	q := fmt.Sprintf(
		"DELETE FROM refresh_tokens WHERE token='%s';",
		sqlEscape(t.Token),
	)
	return r.db.Execute(q)
}

// DeleteAllByUserLogin removes all refresh tokens for the given user.
func (r *SQLiteCloudRefreshTokenRepo) DeleteAllByUserLogin(userLogin string) error {
	q := fmt.Sprintf(
		"DELETE FROM refresh_tokens WHERE user_login='%s';",
		sqlEscape(userLogin),
	)
	return r.db.Execute(q)
}

// DeleteExpiredTokens removes tokens past their expiration time.
// Uses SQLite's datetime('now') instead of PostgreSQL's NOW().
func (r *SQLiteCloudRefreshTokenRepo) DeleteExpiredTokens() error {
	return r.db.Execute("DELETE FROM refresh_tokens WHERE expires_at < datetime('now');")
}
