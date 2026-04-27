package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	formatCommitsStatus   = "github commits status=%d: %s"
	formatListReposStatus = "github list repos status=%d: %s"
	formatPullsStatus     = "github pulls status=%d: %s"
	commitsPageFmt        = "%s/repos/%s/commits?per_page=%d&page=%d"
	pullsPageFmt          = "%s/repos/%s/pulls?per_page=%d&page=%d&state=all"
	userReposFmt          = "%s/user/repos?per_page=%d&page=%d"
)

// CommitFetcher retrieves commit messages from a VCS repository.
type CommitFetcher interface {
	ListCommitMessages(ctx context.Context, ownerRepo, author string, since, until time.Time) ([]CommitInfo, error)
	ListPullRequests(ctx context.Context, ownerRepo, author string, since, until time.Time) ([]PullRequestInfo, error)
}

// CommitFetcherFactory creates a CommitFetcher authenticated with the given token.
type CommitFetcherFactory interface {
	New(token string) CommitFetcher
}

// GitHubClientFactory implements CommitFetcherFactory.
type GitHubClientFactory struct{}

// New returns a CommitFetcher (GitHubClient) authenticated with the given token.
func (GitHubClientFactory) New(token string) CommitFetcher {
	return NewGitHubClient(token)
}

// GitHubClient provides simple helpers to call GitHub APIs with a user token.
type GitHubClient struct {
	client  *http.Client
	baseURL string
	token   string

	// nameOnce guards a single lazy fetch of the authenticated user's full name.
	// Cached for the lifetime of this client instance so N-repo report generation
	// does not trigger N extra /user API calls.
	nameOnce   sync.Once
	cachedName string
}

// NewGitHubClient creates a new GitHubClient using the provided token.
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{
		client:  &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://api.github.com",
		token:   token,
	}
}

// newRequest builds an HTTP request with the common GitHub headers.
func (g *GitHubClient) newRequest(
	ctx context.Context,
	method string,
	url string,
	body io.Reader,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	return req, nil
}

// getAuthenticatedUserName returns the full name of the authenticated GitHub user.
// The result is fetched once and cached for the lifetime of the client (sync.Once),
// so repeated calls within the same request/report generation are free.
// Errors are swallowed intentionally — an empty name simply disables name-based
// author matching, falling back to login matching.
func (g *GitHubClient) getAuthenticatedUserName(ctx context.Context) string {
	if g.token == "" {
		return ""
	}
	g.nameOnce.Do(func() {
		g.cachedName = g.fetchAuthenticatedUserName(ctx)
	})
	return g.cachedName
}

// fetchAuthenticatedUserName performs the actual /user API call.
// Separated from getAuthenticatedUserName so sync.Once wraps only this work.
func (g *GitHubClient) fetchAuthenticatedUserName(ctx context.Context) string {
	userURL := fmt.Sprintf("%s/user", g.baseURL)
	req, err := g.newRequest(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return ""
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return ""
	}
	defer closeBody(resp.Body)
	if resp.StatusCode != 200 {
		return ""
	}
	var u struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return ""
	}
	return strings.TrimSpace(u.Name)
}

type ghRepo struct {
	FullName string `json:"full_name"`
}
type ghCommit struct {
	SHA    string `json:"sha,omitempty"`
	Commit struct {
		Message string `json:"message,omitempty"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date,omitempty"`
		} `json:"author"`
	} `json:"commit"`
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
}

// ListRepositories returns the authenticated user's repositories as owner/name.
func (g *GitHubClient) ListRepositories(ctx context.Context) ([]string, error) {
	var out []string
	perPage := 100

	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		repos, last, err := g.fetchUserReposPage(ctx, perPage, page)
		if err != nil {
			return nil, err
		}
		if len(repos) == 0 {
			break
		}
		for _, r := range repos {
			out = append(out, r.FullName)
		}
		if last {
			break
		}
	}

	return out, nil
}

// fetchUserReposPage fetches a single page of the authenticated user's repos.
// Returns the repos, whether this is the last page, and an error.
func (g *GitHubClient) fetchUserReposPage(ctx context.Context, perPage, page int) ([]ghRepo, bool, error) {
	u := fmt.Sprintf(userReposFmt, g.baseURL, perPage, page)
	req, err := g.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, false, fmt.Errorf(formatListReposStatus, resp.StatusCode, string(b))
	}

	var repos []ghRepo
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, false, err
	}

	if len(repos) == 0 || len(repos) < perPage {
		return repos, true, nil
	}
	return repos, false, nil
}

// countCommitsNoAuthor performs the fast count path when no author filter is provided.
func (g *GitHubClient) countCommitsNoAuthor(ctx context.Context, ownerRepo string, since, until time.Time) (int, error) {
	u := g.commitsPageURL(ownerRepo, 1, 1, since, until)

	req, err := g.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer closeBody(resp.Body)

	if resp.StatusCode == 404 {
		return 0, nil
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf(formatCommitsStatus, resp.StatusCode, string(b))
	}

	link := resp.Header.Get("Link")
	if link != "" {
		if last := parseLastPage(link); last > 0 {
			return last, nil
		}
	}

	var commits []any
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return 0, err
	}
	return len(commits), nil
}

// countCommitsAuthorFast tries the server-side author filter using per_page=1.
// Returns (count, found, err) where found indicates a conclusive result.
func (g *GitHubClient) countCommitsAuthorFast(ctx context.Context, ownerRepo, author string, since, until time.Time) (int, bool, error) {
	u := g.commitsPageURL(ownerRepo, 1, 1, since, until)

	// Build author param via url.Values and append cleanly rather than manual string concat.
	parsed, err := url.Parse(u)
	if err != nil {
		return 0, true, err
	}
	q := parsed.Query()
	q.Set("author", author)
	parsed.RawQuery = q.Encode()
	u = parsed.String()

	req, err := g.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, true, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return 0, true, err
	}
	defer closeBody(resp.Body)

	if resp.StatusCode == 404 {
		return 0, true, nil
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return 0, true, fmt.Errorf(formatCommitsStatus, resp.StatusCode, string(b))
	}

	link := resp.Header.Get("Link")
	if link != "" {
		if last := parseLastPage(link); last > 0 {
			return last, true, nil
		}
	}

	var commits []any
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return 0, true, err
	}
	if len(commits) > 0 {
		return len(commits), true, nil
	}
	return 0, false, nil
}

// countCommitsAuthorFallback iterates pages and applies client-side author matching.
func (g *GitHubClient) countCommitsAuthorFallback(ctx context.Context, ownerRepo, author string, since, until time.Time, myName string) (int, error) {
	perPage := 100
	count := 0
	for page := 1; ; page++ {
		u := g.commitsPageURL(ownerRepo, perPage, page, since, until)

		ghCommits, status, err := g.fetchCommitsPage(ctx, u)
		if err != nil {
			return 0, err
		}
		if status == 404 {
			return 0, nil
		}

		if len(ghCommits) == 0 {
			break
		}

		count += g.countMatchingCommits(ghCommits, author, myName)

		if len(ghCommits) < perPage {
			break
		}
	}
	return count, nil
}

// commitsPageURL builds the commits page URL with optional since/until params.
func (g *GitHubClient) commitsPageURL(ownerRepo string, perPage, page int, since, until time.Time) string {
	base := fmt.Sprintf(commitsPageFmt, g.baseURL, ownerRepo, perPage, page)
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := parsed.Query()
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	if !until.IsZero() {
		q.Set("until", until.UTC().Format(time.RFC3339))
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

// countMatchingCommits counts commits that match the author using existing heuristics.
func (g *GitHubClient) countMatchingCommits(ghCommits []ghCommit, author, myName string) int {
	cnt := 0
	for _, c := range ghCommits {
		if g.commitMatchesAuthor(c, author, myName) {
			cnt++
		}
	}
	return cnt
}

// CountCommits estimates the number of commits by using pagination info.
// ownerRepo must be "owner/repo". Author is the GitHub login (author filter).
//
//nolint:gocyclo
func (g *GitHubClient) CountCommits(ctx context.Context, ownerRepo, author string, since, until time.Time) (int, error) {
	if author == "" {
		return g.countCommitsNoAuthor(ctx, ownerRepo, since, until)
	}
	myName := g.getAuthenticatedUserName(ctx)
	if cnt, found, err := g.countCommitsAuthorFast(ctx, ownerRepo, author, since, until); err != nil {
		return 0, err
	} else if found {
		return cnt, nil
	}
	return g.countCommitsAuthorFallback(ctx, ownerRepo, author, since, until, myName)
}

func parseLastPage(link string) int {
	for _, p := range strings.Split(link, ",") {
		p = strings.TrimSpace(p)
		if !strings.Contains(p, `rel="last"`) {
			continue
		}
		if v := pageFromLinkPart(p); v > 0 {
			return v
		}
	}
	return 0
}

// pageFromLinkPart extracts the `page` query parameter value from a single
// link header part like `<https://.../commits?page=10>; rel="last"`.
func pageFromLinkPart(part string) int {
	start := strings.Index(part, "<")
	end := strings.Index(part, ">")
	if start == -1 || end == -1 || end <= start+1 {
		return 0
	}
	u := part[start+1 : end]
	parsed, err := url.Parse(u)
	if err != nil {
		return 0
	}
	if p := parsed.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			return v
		}
	}
	return 0
}

// fetchCommitsPage performs a GET for the given commits URL and decodes the
// response into a slice of ghCommit. Returns the HTTP status code so callers
// can distinguish 404 (not found) from other errors.
func (g *GitHubClient) fetchCommitsPage(ctx context.Context, u string) ([]ghCommit, int, error) {
	req, err := g.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer closeBody(resp.Body)

	if resp.StatusCode == 404 {
		return nil, 404, nil
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf(formatCommitsStatus, resp.StatusCode, string(b))
	}

	var ghCommits []ghCommit
	if err := json.NewDecoder(resp.Body).Decode(&ghCommits); err != nil {
		return nil, resp.StatusCode, err
	}
	return ghCommits, resp.StatusCode, nil
}

// commitMatchesAuthor applies client-side heuristics to determine whether
// the commit should be counted for the requested author.
func (g *GitHubClient) commitMatchesAuthor(c ghCommit, author, myName string) bool {
	if author == "" {
		return true
	}
	commitName := strings.TrimSpace(c.Commit.Author.Name)
	loginMatch := c.Author != nil && strings.EqualFold(c.Author.Login, author)
	nameEqualMatch := commitName != "" && (strings.EqualFold(commitName, author) || (myName != "" && strings.EqualFold(commitName, myName)))
	containsMatch := false
	if myName != "" && commitName != "" {
		lm := strings.ToLower(myName)
		cm := strings.ToLower(commitName)
		if strings.Contains(lm, cm) || strings.Contains(cm, lm) {
			containsMatch = true
		}
	}
	return loginMatch || nameEqualMatch || containsMatch
}

// CommitInfo represents a simplified commit payload extracted from GitHub API.
type CommitInfo struct {
	SHA         string    `json:"sha"`
	Message     string    `json:"message"`
	AuthorLogin string    `json:"authorLogin,omitempty"`
	AuthorName  string    `json:"authorName,omitempty"`
	Date        time.Time `json:"date"`
}

// ghCommitToCommitInfo converts a ghCommit into the public CommitInfo shape.
func ghCommitToCommitInfo(c ghCommit) CommitInfo {
	var d time.Time
	if t := strings.TrimSpace(c.Commit.Author.Date); t != "" {
		d, _ = time.Parse(time.RFC3339, t)
	}
	ci := CommitInfo{
		SHA:        c.SHA,
		Message:    c.Commit.Message,
		AuthorName: strings.TrimSpace(c.Commit.Author.Name),
		Date:       d,
	}
	if c.Author != nil {
		ci.AuthorLogin = c.Author.Login
	}
	return ci
}

// filterAndConvertCommits converts a slice of ghCommit to CommitInfo applying
// the author filter (client-side).
func (g *GitHubClient) filterAndConvertCommits(ghCommits []ghCommit, author, myName string) []CommitInfo {
	out := make([]CommitInfo, 0, len(ghCommits))
	for _, c := range ghCommits {
		if author == "" || g.commitMatchesAuthor(c, author, myName) {
			out = append(out, ghCommitToCommitInfo(c))
		}
	}
	return out
}

type ghPull struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	ClosedAt  string `json:"closed_at"`
	MergedAt  string `json:"merged_at"`
	State     string `json:"state"`
	HTMLURL   string `json:"html_url"`
}

// PullRequestInfo represents a simplified pull request payload extracted from GitHub API.
type PullRequestInfo struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body,omitempty"`
	AuthorLogin string    `json:"authorLogin,omitempty"`
	CreatedAt   time.Time `json:"createdAt,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
	ClosedAt    time.Time `json:"closedAt,omitempty"`
	MergedAt    time.Time `json:"mergedAt,omitempty"`
	State       string    `json:"state,omitempty"`
	URL         string    `json:"url,omitempty"`
}

func ghPullToPRInfo(p ghPull) PullRequestInfo {
	var created, updated, closed, merged time.Time
	if t := strings.TrimSpace(p.CreatedAt); t != "" {
		created, _ = time.Parse(time.RFC3339, t)
	}
	if t := strings.TrimSpace(p.UpdatedAt); t != "" {
		updated, _ = time.Parse(time.RFC3339, t)
	}
	if t := strings.TrimSpace(p.ClosedAt); t != "" {
		closed, _ = time.Parse(time.RFC3339, t)
	}
	if t := strings.TrimSpace(p.MergedAt); t != "" {
		merged, _ = time.Parse(time.RFC3339, t)
	}
	return PullRequestInfo{
		Number:      p.Number,
		Title:       strings.TrimSpace(p.Title),
		Body:        strings.TrimSpace(p.Body),
		AuthorLogin: p.User.Login,
		CreatedAt:   created,
		UpdatedAt:   updated,
		ClosedAt:    closed,
		MergedAt:    merged,
		State:       p.State,
		URL:         p.HTMLURL,
	}
}

func (g *GitHubClient) fetchPullsPage(ctx context.Context, u string) ([]ghPull, int, error) {
	req, err := g.newRequest(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer closeBody(resp.Body)

	if resp.StatusCode == 404 {
		return nil, 404, nil
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf(formatPullsStatus, resp.StatusCode, string(b))
	}

	var ghPulls []ghPull
	if err := json.NewDecoder(resp.Body).Decode(&ghPulls); err != nil {
		return nil, resp.StatusCode, err
	}
	return ghPulls, resp.StatusCode, nil
}

func isBetween(t, since, until time.Time) bool {
	if since.IsZero() && until.IsZero() {
		return true
	}
	if !since.IsZero() && t.Before(since) {
		return false
	}
	if !until.IsZero() && t.After(until) {
		return false
	}
	return true
}

// fetchAllPulls paginates through the pulls API and returns all results.
func (g *GitHubClient) fetchAllPulls(ctx context.Context, ownerRepo string, perPage int) ([]ghPull, error) {
	var all []ghPull
	for page := 1; ; page++ {
		u := fmt.Sprintf(pullsPageFmt, g.baseURL, ownerRepo, perPage, page)
		ghPulls, status, err := g.fetchPullsPage(ctx, u)
		if err != nil {
			return nil, err
		}
		if status == 404 {
			return nil, nil
		}
		if len(ghPulls) == 0 {
			break
		}
		all = append(all, ghPulls...)
		if len(ghPulls) < perPage {
			break
		}
	}
	return all, nil
}

// prMatchesFilters applies author and date-range filters to a PullRequestInfo.
func prMatchesFilters(pr PullRequestInfo, author string, since, until time.Time) bool {
	if author != "" && !strings.EqualFold(pr.AuthorLogin, author) {
		return false
	}
	if since.IsZero() && until.IsZero() {
		return true
	}
	if !pr.CreatedAt.IsZero() && isBetween(pr.CreatedAt, since, until) {
		return true
	}
	if !pr.MergedAt.IsZero() && isBetween(pr.MergedAt, since, until) {
		return true
	}
	return false
}

// ListPullRequests returns pull requests for the given repository filtered by
// author and date range (created_at and merged_at, inclusive).
func (g *GitHubClient) ListPullRequests(ctx context.Context, ownerRepo, author string, since, until time.Time) ([]PullRequestInfo, error) {
	perPage := 100
	ghPulls, err := g.fetchAllPulls(ctx, ownerRepo, perPage)
	if err != nil {
		return nil, err
	}
	if ghPulls == nil {
		return nil, nil
	}

	out := make([]PullRequestInfo, 0, len(ghPulls))
	for _, p := range ghPulls {
		pr := ghPullToPRInfo(p)
		if !prMatchesFilters(pr, author, since, until) {
			continue
		}
		out = append(out, pr)
	}
	return out, nil
}

// ListCommitMessages returns commits (sha, message, author, date) for the
// given repository. Server-side author filter is tried first; falls back to
// client-side filtering on empty results.
//
//nolint:gocyclo
func (g *GitHubClient) ListCommitMessages(ctx context.Context, ownerRepo, author string, since, until time.Time) ([]CommitInfo, error) {
	perPage := 100
	myName := g.getAuthenticatedUserName(ctx)

	if author != "" {
		if out, found, err := g.listCommitMessagesServerSide(ctx, ownerRepo, author, since, until, perPage); err != nil {
			return nil, err
		} else if found {
			return out, nil
		}
	}

	return g.listCommitMessagesFullScan(ctx, ownerRepo, author, since, until, myName, perPage)
}

// listCommitMessagesServerSide queries commits using the server-side author filter.
func (g *GitHubClient) listCommitMessagesServerSide(ctx context.Context, ownerRepo, author string, since, until time.Time, perPage int) ([]CommitInfo, bool, error) {
	var out []CommitInfo
	for page := 1; ; page++ {
		base := g.commitsPageURL(ownerRepo, perPage, page, since, until)
		parsed, err := url.Parse(base)
		if err != nil {
			return nil, true, err
		}
		q := parsed.Query()
		q.Set("author", author)
		parsed.RawQuery = q.Encode()

		ghCommits, status, err := g.fetchCommitsPage(ctx, parsed.String())
		if err != nil {
			return nil, true, err
		}
		if status == 404 {
			return nil, true, nil
		}

		if len(ghCommits) == 0 {
			break
		}
		for _, c := range ghCommits {
			out = append(out, ghCommitToCommitInfo(c))
		}
		if len(ghCommits) < perPage {
			break
		}
	}
	if len(out) > 0 {
		return out, true, nil
	}
	return nil, false, nil
}

// listCommitMessagesFullScan iterates all commits and applies client-side filtering.
func (g *GitHubClient) listCommitMessagesFullScan(ctx context.Context, ownerRepo, author string, since, until time.Time, myName string, perPage int) ([]CommitInfo, error) {
	var out []CommitInfo
	for page := 1; ; page++ {
		u := g.commitsPageURL(ownerRepo, perPage, page, since, until)

		ghCommits, status, err := g.fetchCommitsPage(ctx, u)
		if err != nil {
			return nil, err
		}
		if status == 404 {
			return nil, nil
		}

		if len(ghCommits) == 0 {
			break
		}

		out = append(out, g.filterAndConvertCommits(ghCommits, author, myName)...)

		if len(ghCommits) < perPage {
			break
		}
	}
	return out, nil
}
