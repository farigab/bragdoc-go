// Package main is the server entrypoint for the bragdev application.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	sqlitecloud "github.com/sqlitecloud/sqlitecloud-go"

	"github.com/farigab/bragdev-go/internal/config"
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

	// Initialize logger early so startup messages are visible and consistent
	logger.Init(cfg.LogLevel)

	if cfg.SQLiteCloudURL == "" {
		logger.Errorf("SQLITECLOUD_URL environment variable must be set")
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

	// Optionally run migrations if MIGRATE=true
	if strings.ToLower(os.Getenv("MIGRATE")) == "true" {
		if err := runMigrations(db, "db/migrations"); err != nil {
			logger.Errorf("migrations failed: %v", err)
			os.Exit(1)
		}
	}

	r := chi.NewRouter()

	// CORS middleware
	r.Use(appMiddleware.CORSMiddleware(cfg))

	// Request logging + panic recovery
	r.Use(appMiddleware.RequestLogger)

	// health
	r.Get("/api/health", handlers.HealthHandler)

	// wiring
	userRepo := repository.NewUserRepo(db)
	refreshRepo := repository.NewRefreshTokenRepo(db)

	jwtSvc := security.NewJWTService(cfg.JwtSecret, 900) // 15 minutes
	oauthSvc := integration.NewGitHubOAuthService(cfg.GitHubClientID, cfg.GitHubClientSecret)
	geminiClient := integration.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiAPIURL, cfg.GeminiModel)
	fetcherFactory := integration.GitHubClientFactory{}
	reportSvc := usecase.NewReportService(userRepo, fetcherFactory, geminiClient)

	// Public auth routes (OAuth flow + token refresh)
	handlers.RegisterAuthRoutes(r, cfg, oauthSvc, jwtSvc, userRepo, refreshRepo)

	// Protected routes — require a valid JWT cookie
	r.Group(func(r chi.Router) {
		r.Use(appMiddleware.AuthWithRefresh(cfg, jwtSvc, userRepo, refreshRepo))
		handlers.RegisterUserRoutes(r, userRepo)
		handlers.RegisterGitHubRoutes(r, userRepo)
		handlers.RegisterReportRoutes(r, reportSvc)
	})

	// Background job: cleanup expired refresh tokens every hour
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := refreshRepo.DeleteExpiredTokens(); err != nil {
				logger.Errorw("failed cleaning expired refresh tokens", "err", err)
			}
		}
	}()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logger.Infow("starting server", "port", port)
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		logger.Errorf("failed to bind port: %v", err)
		os.Exit(1)
	}

	logger.Infow("server started", "port", port)
	if err := http.Serve(ln, r); err != nil {
		logger.Errorf("server error: %v", err)
		os.Exit(1)
	}
}

func runMigrations(db *sqlitecloud.SQCloud, dir string) error {
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
		if err := applyMigration(db, dir, n); err != nil {
			return err
		}
		logger.Infow("applied migration", "migration", n)
	}
	return nil
}

// applyMigration reads and executes all statements in a single .sql file.
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

// splitStatements splits a SQL script into individual statements by semicolon,
// ignoring semicolons that appear inside single-quoted strings.
func splitStatements(sql string) []string {
	var stmts []string
	var cur strings.Builder
	inQuote := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch {
		case ch == '\'' && !inQuote:
			inQuote = true
			cur.WriteByte(ch)
		case ch == '\'' && inQuote:
			// Handle escaped single quote ('')
			if i+1 < len(sql) && sql[i+1] == '\'' {
				cur.WriteByte(ch)
				i++
				cur.WriteByte(ch)
			} else {
				inQuote = false
				cur.WriteByte(ch)
			}
		case ch == ';' && !inQuote:
			stmts = append(stmts, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}
