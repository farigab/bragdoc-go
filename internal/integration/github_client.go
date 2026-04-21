package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	acceptHeader          = "application/vnd.github+json"
	userAgent             = "bragdoc-app"
	authPrefix            = "token "
	formatCommitsStatus   = "github commits status=%d: %s"
	formatListReposStatus = "github list repos status=%d: %s"
	commitsPageFmt        = "%s/repos/%s/commits?per_page=%d&page=%d"
	userReposFmt          = "%s/user/repos?per_page=%d&page=%d"
)

// CommitFetcher retrieves commit messages from a VCS repository.
type CommitFetcher interface {
	ListCommitMessages(ownerRepo, author string, since, until time.Time) ([]CommitInfo, error)
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
func (g *GitHubClient) newRequest(method, u string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", acceptHeader)
	if g.token != "" {
		req.Header.Set("Authorization", authPrefix+g.token)
	}
	req.Header.Set("User-Agent", userAgent)
	return req, nil
}

// getAuthenticatedUserName tries to fetch the authenticated user's full name.
// It intentionally swallows errors and returns empty string on failure to
// preserve the original tolerant behavior.
func (g *GitHubClient) getAuthenticatedUserName() string {
	if g.token == "" {
		return ""
	}
	userURL := fmt.Sprintf("%s/user", g.baseURL)
	reqUser, err := g.newRequest("GET", userURL, nil)
	if err != nil {
		return ""
	}
	respUser, err := g.client.Do(reqUser)
	if err != nil {
		return ""
	}
	defer closeBody(respUser.Body)
	if respUser.StatusCode != 200 {
		return ""
	}
	var u struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(respUser.Body).Decode(&u); err != nil {
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
func (g *GitHubClient) ListRepositories() ([]string, error) {
	var out []string
	perPage := 100
	for page := 1; ; page++ {
		u := fmt.Sprintf(userReposFmt, g.baseURL, perPage, page)
		req, err := g.newRequest("GET", u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := g.client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			closeBody(resp.Body)
			return nil, fmt.Errorf(formatListReposStatus, resp.StatusCode, string(b))
		}

		var repos []ghRepo
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			closeBody(resp.Body)
			return nil, err
		}
		closeBody(resp.Body)
		if len(repos) == 0 {
			break
		}
		for _, r := range repos {
			out = append(out, r.FullName)
		}
		if len(repos) < perPage {
			break
		}
	}
	return out, nil
}

// countCommitsNoAuthor performs the fast count path when no author filter is provided.
func (g *GitHubClient) countCommitsNoAuthor(ownerRepo string, since, until time.Time) (int, error) {
	u := g.commitsPageURL(ownerRepo, 1, 1, since, until)

	req, err := g.newRequest("GET", u, nil)
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
// It returns (count, found, err) where found indicates whether the fast
// attempt produced a conclusive result (even if zero).
func (g *GitHubClient) countCommitsAuthorFast(ownerRepo, author string, since, until time.Time) (int, bool, error) {
	u := g.commitsPageURL(ownerRepo, 1, 1, since, until)
	params := url.Values{}
	params.Set("author", author)
	if params.Encode() != "" {
		u = u + "&" + params.Encode()
	}

	req, err := g.newRequest("GET", u, nil)
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
func (g *GitHubClient) countCommitsAuthorFallback(ownerRepo, author string, since, until time.Time, myName string) (int, error) {
	perPage := 100
	count := 0
	for page := 1; ; page++ {
		u := g.commitsPageURL(ownerRepo, perPage, page, since, until)

		ghCommits, status, err := g.fetchCommitsPage(u)
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
	u := fmt.Sprintf(commitsPageFmt, g.baseURL, ownerRepo, perPage, page)
	q := url.Values{}
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	if !until.IsZero() {
		q.Set("until", until.UTC().Format(time.RFC3339))
	}
	if q.Encode() != "" {
		u = u + "&" + q.Encode()
	}
	return u
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
func (g *GitHubClient) CountCommits(ownerRepo, author string, since, until time.Time) (int, error) {
	if author == "" {
		return g.countCommitsNoAuthor(ownerRepo, since, until)
	}
	myName := g.getAuthenticatedUserName()
	if cnt, found, err := g.countCommitsAuthorFast(ownerRepo, author, since, until); err != nil {
		return 0, err
	} else if found {
		return cnt, nil
	}
	return g.countCommitsAuthorFallback(ownerRepo, author, since, until, myName)
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
// response into a slice of ghCommit. It returns the HTTP status code so callers
// can distinguish 404 (not found) from other errors.
func (g *GitHubClient) fetchCommitsPage(u string) ([]ghCommit, int, error) {
	req, err := g.newRequest("GET", u, nil)
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

// commitMatchesAuthor applies the client-side heuristics to determine whether
// the provided commit should be counted for the requested author.
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
// the author filter (client-side). This keeps the pagination loop simple and
// reduces cognitive complexity in callers.
func (g *GitHubClient) filterAndConvertCommits(ghCommits []ghCommit, author, myName string) []CommitInfo {
	out := make([]CommitInfo, 0, len(ghCommits))
	for _, c := range ghCommits {
		if author == "" || g.commitMatchesAuthor(c, author, myName) {
			out = append(out, ghCommitToCommitInfo(c))
		}
	}
	return out
}

// ListCommitMessages returns commits (sha, message, author, date) for the
// given repository. If author is provided, it will try a server-side filter
// first and then fall back to client-side filtering similar to CountCommits.
//
//nolint:gocyclo
func (g *GitHubClient) ListCommitMessages(ownerRepo, author string, since, until time.Time) ([]CommitInfo, error) {
	// Use smaller helpers to keep complexity low.
	perPage := 100
	myName := g.getAuthenticatedUserName()

	if author != "" {
		if out, found, err := g.listCommitMessagesServerSide(ownerRepo, author, since, until, perPage); err != nil {
			return nil, err
		} else if found {
			return out, nil
		}
		// else fall through to full scan
	}

	return g.listCommitMessagesFullScan(ownerRepo, author, since, until, myName, perPage)
}

// listCommitMessagesServerSide queries commits using the server-side author filter.
// Returns (results, found, err) where found indicates the server-side attempt
// gave a conclusive result (even if zero).
func (g *GitHubClient) listCommitMessagesServerSide(ownerRepo, author string, since, until time.Time, perPage int) ([]CommitInfo, bool, error) {
	var out []CommitInfo
	for page := 1; ; page++ {
		u := g.commitsPageURL(ownerRepo, perPage, page, since, until)
		params := url.Values{}
		params.Set("author", author)
		if params.Encode() != "" {
			u = u + "&" + params.Encode()
		}

		ghCommits, status, err := g.fetchCommitsPage(u)
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
func (g *GitHubClient) listCommitMessagesFullScan(ownerRepo, author string, since, until time.Time, myName string, perPage int) ([]CommitInfo, error) {
	var out []CommitInfo
	for page := 1; ; page++ {
		u := g.commitsPageURL(ownerRepo, perPage, page, since, until)

		ghCommits, status, err := g.fetchCommitsPage(u)
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
