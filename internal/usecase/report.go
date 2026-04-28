// Package usecase contains business logic implementations used by handlers.
package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"bragdev-go/internal/integration"
	"bragdev-go/internal/report"
	"bragdev-go/internal/repository"
)

// GenerateReportInput holds the parameters for report generation.
type GenerateReportInput struct {
	UserLogin    string
	ReportType   string
	Category     string
	StartDate    time.Time
	EndDate      time.Time
	UserPrompt   string
	Repositories []string
}

// ReportService encapsulates report generation business logic.
type ReportService struct {
	userRepo       repository.UserRepository
	fetcherFactory integration.CommitFetcherFactory
	ai             integration.AIReportGenerator
}

// NewReportService constructs a ReportService with required dependencies.
func NewReportService(
	userRepo repository.UserRepository,
	fetcherFactory integration.CommitFetcherFactory,
	ai integration.AIReportGenerator,
) *ReportService {
	return &ReportService{
		userRepo:       userRepo,
		fetcherFactory: fetcherFactory,
		ai:             ai,
	}
}

// Generate collects GitHub commit data, builds an AI prompt, and returns the
// generated report text.
func (s *ReportService) Generate(ctx context.Context, in GenerateReportInput) (string, error) {
	filtered, err := s.collectCommitData(ctx, in)
	if err != nil {
		return "", err
	}

	achievementsDataBytes, err := json.Marshal(filtered)
	if err != nil {
		return "", fmt.Errorf("failed to serialize data: %w", err)
	}

	prompt := report.BuildPrompt(string(achievementsDataBytes), in.ReportType)
	if in.UserPrompt != "" {
		prompt = in.UserPrompt + "\n\n" + prompt
	}

	return s.ai.GenerateReport(ctx, prompt)
}

// collectCommitData fetches commits for every non-empty repository.
// Repos that error or return no data are silently skipped.
func (s *ReportService) collectCommitData(ctx context.Context, in GenerateReportInput) ([]any, error) {
	repos := nonEmptyRepos(in.Repositories)
	if len(repos) == 0 {
		return []any{}, nil
	}

	u, err := s.userRepo.FindByLogin(ctx, in.UserLogin)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	fetcher := s.fetcherFactory.New(strings.TrimSpace(u.GitHubAccessToken))
	filtered := make([]any, 0, len(repos))

	for _, repo := range repos {
		commits, cerr := fetcher.ListCommitMessages(ctx, repo, in.UserLogin, in.StartDate, in.EndDate)
		prs, perr := fetcher.ListPullRequests(ctx, repo, in.UserLogin, in.StartDate, in.EndDate)

		if (cerr != nil && perr != nil) || (len(commits) == 0 && len(prs) == 0) {
			continue
		}

		filtered = append(filtered, map[string]any{
			"repo":         repo,
			"commits":      commits,
			"pullRequests": prs,
		})
	}

	return filtered, nil
}

func nonEmptyRepos(repos []string) []string {
	result := make([]string, 0, len(repos))
	for _, r := range repos {
		if strings.TrimSpace(r) != "" {
			result = append(result, r)
		}
	}
	return result
}
