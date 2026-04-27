// Package main is the server entrypoint for the bragdev application.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	sqlitecloud "github.com/sqlitecloud/sqlitecloud-go"

	"github.com/farigab/bragdev-go/internal/config"
	appdb "github.com/farigab/bragdev-go/internal/db"
	"github.com/farigab/bragdev-go/internal/handlers"
	"github.com/farigab/bragdev-go/internal/integration"
	"github.com/farigab/bragdev-go/internal/logger"
	appMiddleware "github.com/farigab/bragdev-go/internal/middleware"
	"github.com/farigab/bragdev-go/internal/repository"
	"github.com/farigab/bragdev-go/internal/security"
	"github.com/farigab/bragdev-go/internal/usecase"
)

func main() {
	cfg := config.Load()
	logger.Init(cfg.LogLevel)

	if cfg.SQLiteCloudURL == "" {
		logger.Errorf("DB_URL environment variable must be set")
		os.Exit(1)
	}

	db, err := sqlitecloud.Connect(cfg.SQLiteCloudURL)
	if err != nil {
		logger.Errorf("failed to connect to SQLite Cloud: %v", err)
		os.Exit(1)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			logger.Errorf("failed to close db: %v", cerr)
		}
	}()

	logger.Infow("connected to SQLite Cloud")

	if cfg.Migrate {
		if err := runMigrations(db, "db/migrations"); err != nil {
			logger.Errorf("migrations failed: %v", err)
			os.Exit(1)
		}
	}

	// NewJWTService now returns an error instead of calling os.Exit internally,
	// so test code can construct a JWTService without process termination.
	jwtSvc, err := security.NewJWTService(cfg.JwtSecret, 900)
	if err != nil {
		logger.Errorf("jwt service: %v", err)
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(appMiddleware.CORSMiddleware(cfg))
	r.Use(appMiddleware.RequestLogger)

	r.Get("/api/health", handlers.HealthHandler)

	appDB := appdb.New(db)
	userRepo := repository.NewUserRepo(appDB)
	refreshRepo := repository.NewRefreshTokenRepo(appDB)

	oauthSvc := integration.NewGitHubOAuthService(cfg.GitHubClientID, cfg.GitHubClientSecret)
	geminiClient := integration.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiAPIURL, cfg.GeminiModel)
	fetcherFactory := integration.GitHubClientFactory{}
	reportSvc := usecase.NewReportService(userRepo, fetcherFactory, geminiClient)

	// Public auth routes (OAuth flow + refresh)
	handlers.RegisterAuthRoutes(r, cfg, oauthSvc, jwtSvc, userRepo, refreshRepo)

	// Protected routes — require a valid JWT or valid refreshToken
	r.Group(func(r chi.Router) {
		r.Use(appMiddleware.AuthWithRefresh(cfg, jwtSvc, userRepo, refreshRepo))
		handlers.RegisterUserRoutes(r, userRepo)
		handlers.RegisterGitHubRoutes(r, userRepo)
		handlers.RegisterReportRoutes(r, reportSvc)
		// GitHub token routes were previously registered as public with manual
		// cookie validation — they now live in the protected group and rely on
		// the AuthWithRefresh context like every other protected handler.
		handlers.RegisterTokenRoutes(r, userRepo)
	})

	// Background cleanup: stop cleanly when the server shuts down.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runTokenCleanup(ctx, refreshRepo)

	port := portFromEnv()
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Errorf("failed to bind port: %v", err)
		os.Exit(1)
	}

	srv := &http.Server{Handler: r}
	logger.Infow("server started", "port", port)

	// Graceful shutdown on SIGTERM / SIGINT (critical for containerised deploys).
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
		<-sigCh
		logger.Infow("shutdown signal received")
		cancel() // stop background goroutines
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			logger.Errorf("graceful shutdown error: %v", err)
		}
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		logger.Errorf("server error: %v", err)
		os.Exit(1)
	}
	logger.Infow("server stopped")
}

// runTokenCleanup deletes expired refresh tokens every hour until ctx is cancelled.
func runTokenCleanup(ctx context.Context, repo repository.RefreshTokenRepository) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := repo.DeleteExpiredTokens(ctx); err != nil {
				logger.Errorw("failed cleaning expired refresh tokens", "err", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

// Port method removed — use portFromEnv() local helper instead.

func portFromEnv() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "8080"
}

// ─── Migrations ──────────────────────────────────────────────────────────────

// runMigrations applies pending .sql files from dir in lexicographic order.
// It creates a schema_migrations tracking table on first run so each file is
// only applied once — without tracking, idempotent CREATE IF NOT EXISTS worked
// by accident but any future ALTER/INSERT migration would run multiple times.
func runMigrations(db *sqlitecloud.SQCloud, dir string) error {
	if err := db.Execute(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	);`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, n := range names {
		applied, err := isMigrationApplied(db, n)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", n, err)
		}
		if applied {
			logger.Infow("skipping migration (already applied)", "migration", n)
			continue
		}

		if err := applyMigration(db, dir, n); err != nil {
			return err
		}

		if err := markMigrationApplied(db, n); err != nil {
			return fmt.Errorf("mark migration %s: %w", n, err)
		}
		logger.Infow("applied migration", "migration", n)
	}
	return nil
}

func isMigrationApplied(db *sqlitecloud.SQCloud, filename string) (bool, error) {
	q := fmt.Sprintf("SELECT 1 FROM schema_migrations WHERE filename='%s';", sqlEscapeMain(filename))
	result, err := db.Select(q)
	if err != nil {
		return false, err
	}
	return result != nil && result.GetNumberOfRows() > 0, nil
}

func markMigrationApplied(db *sqlitecloud.SQCloud, filename string) error {
	q := fmt.Sprintf(
		"INSERT INTO schema_migrations (filename, applied_at) VALUES ('%s', '%s');",
		sqlEscapeMain(filename),
		time.Now().UTC().Format("2006-01-02 15:04:05"),
	)
	return db.Execute(q)
}

func applyMigration(db *sqlitecloud.SQCloud, dir, name string) error {
	b, err := os.ReadFile(dir + "/" + name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}
	for _, stmt := range splitStatements(string(b)) {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := db.Execute(stmt); err != nil {
			return fmt.Errorf("exec migration %s: %w", name, err)
		}
	}
	return nil
}

// sqlEscapeMain is a local copy of sqlEscape used only for migration filenames.
// Filenames come from ReadDir and are not user-supplied, but we keep it safe.
func sqlEscapeMain(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// splitStatements splits a SQL script on semicolons, correctly handling
// single-quoted strings, -- line comments, and /* */ block comments.
func splitStatements(sql string) []string {
	var stmts []string
	var cur strings.Builder
	i := 0

	for i < len(sql) {
		// Line comment: skip to end of line
		if i+1 < len(sql) && sql[i] == '-' && sql[i+1] == '-' {
			i = skipLineComment(sql, i)
			continue
		}

		// Block comment: skip to closing */
		if i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*' {
			i = skipBlockComment(sql, i)
			continue
		}

		// Quoted string: write quoted content including escaped quotes
		if sql[i] == '\'' {
			i = writeQuoted(sql, i, &cur)
			continue
		}

		// Statement separator outside quotes
		if sql[i] == ';' {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			cur.Reset()
			i++
			continue
		}

		cur.WriteByte(sql[i])
		i++
	}

	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}

// skipLineComment advances i to the end of the current line or EOF.
func skipLineComment(s string, i int) int {
	for i < len(s) && s[i] != '\n' {
		i++
	}
	return i
}

// skipBlockComment advances i past a /* ... */ block or to EOF.
func skipBlockComment(s string, i int) int {
	i += 2 // consume /*
	for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
		i++
	}
	if i+1 < len(s) {
		i += 2 // consume */
	} else {
		i = len(s)
	}
	return i
}

// writeQuoted writes a single-quoted SQL string to cur, handling escaped
// single quotes (”), and returns the index after the closing quote.
func writeQuoted(s string, i int, cur *strings.Builder) int {
	cur.WriteByte('\'')
	i++
	for i < len(s) {
		ch := s[i]
		if ch == '\'' {
			// Escaped quote '' -> write both and continue
			if i+1 < len(s) && s[i+1] == '\'' {
				cur.WriteByte('\'')
				cur.WriteByte('\'')
				i += 2
				continue
			}
			// Closing quote
			cur.WriteByte('\'')
			i++
			break
		}
		cur.WriteByte(ch)
		i++
	}
	return i
}
