package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"bragdev-go/internal/httpresp"
	"bragdev-go/internal/integration"
	"bragdev-go/internal/logger"
	"bragdev-go/internal/middleware"
	"bragdev-go/internal/repository"
	"bragdev-go/internal/validation"
)

// RegisterGitHubRoutes registers GitHub-related endpoints.
// The router r must already have the Auth middleware applied.
func RegisterGitHubRoutes(r chi.Router, userRepo repository.UserRepository) {
	r.Post("/api/github/import/repositories", listRepositoriesHandler(userRepo))
	r.Post("/api/github/import", importRepositoriesHandler(userRepo))
}

func listRepositoriesHandler(userRepo repository.UserRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		userLogin, ok := middleware.UserLoginFromContext(req.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		u, err := userRepo.FindByLogin(req.Context(), userLogin)
		if err != nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		if strings.TrimSpace(u.GitHubAccessToken) == "" {
			http.Error(w, "github token not found", http.StatusBadRequest)
			return
		}

		client := integration.NewGitHubClient(u.GitHubAccessToken)
		repos, err := client.ListRepositories(req.Context())
		if err != nil {
			logger.Errorw("list repos error", "err", err)
			httpresp.JSONError(w, http.StatusBadGateway, "Failed to list repositories")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repos)
	}
}

func importRepositoriesHandler(userRepo repository.UserRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		userLogin, ok := middleware.UserLoginFromContext(req.Context())
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		u, err := userRepo.FindByLogin(req.Context(), userLogin)
		if err != nil {
			httpresp.JSONError(w, http.StatusNotFound, "user not found")
			return
		}
		if strings.TrimSpace(u.GitHubAccessToken) == "" {
			http.Error(w, "github token not found", http.StatusBadRequest)
			return
		}

		req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
		resp, status, err := doImportRepositories(req.Context(), req.Body, u.GitHubAccessToken, userLogin)
		if err != nil {
			if status == http.StatusBadRequest {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			logger.Errorw("import repos error", "err", err)
			http.Error(w, "failed to import repositories", status)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func doImportRepositories(ctx context.Context, body io.Reader, accessToken, userLogin string) (map[string]any, int, error) {
	var payload struct {
		Repositories []string `json:"repositories"`
		DataInicio   string   `json:"dataInicio"`
		DataFim      string   `json:"dataFim"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, http.StatusBadRequest, err
	}

	// Use the shared validation package — previously parseDate silently swallowed
	// parse errors returning time.Time{} as if no date was provided.
	since, until, err := validation.ValidateDateRange(payload.DataInicio, payload.DataFim)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}

	client := integration.NewGitHubClient(accessToken)
	repos := payload.Repositories
	if len(repos) == 0 {
		var err error
		repos, err = client.ListRepositories(ctx)
		if err != nil {
			logger.Errorw("list repos error", "err", err)
			return nil, http.StatusInternalServerError, err
		}
	}

	details := map[string]int{}
	total := 0
	for _, repoFull := range repos {
		c, err := client.CountCommits(ctx, repoFull, userLogin, since, until)
		if err != nil {
			logger.Errorw("count commits error", "repo", repoFull, "err", err)
			continue
		}
		details[repoFull] = c
		total += c
	}

	return map[string]any{
		"repositories": repos,
		"totalCommits": total,
		"details":      details,
	}, http.StatusOK, nil
}
