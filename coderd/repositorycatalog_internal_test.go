package coderd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/coder/coder/v2/codersdk"
)

func TestParseGitCloneURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		raw       string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{name: "ssh form", raw: "git@github.com:Observability360/OmniRoute.git", wantOwner: "Observability360", wantRepo: "OmniRoute", wantOK: true},
		{name: "ssh form no .git suffix", raw: "git@github.com:Observability360/O360-AI-Memory", wantOwner: "Observability360", wantRepo: "O360-AI-Memory", wantOK: true},
		{name: "https form", raw: "https://github.com/Observability360/coder.git", wantOwner: "Observability360", wantRepo: "coder", wantOK: true},
		{name: "ssh:// form", raw: "ssh://git@github.com/Observability360/O360-Stack-Lab.git", wantOwner: "Observability360", wantRepo: "O360-Stack-Lab", wantOK: true},
		{name: "unrelated host rejected", raw: "git@gitlab.com:Observability360/coder.git", wantOK: false},
		{name: "empty", raw: "", wantOK: false},
		{name: "malformed", raw: "not-a-url-at-all", wantOK: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			owner, repo, ok := parseGitCloneURL(tc.raw)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.wantOwner, owner)
				assert.Equal(t, tc.wantRepo, repo)
			}
		})
	}
}

func TestRepositoryWorkspaceName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		repoName string
		want     string
		wantErr  bool
	}{
		{repoName: "OmniRoute", want: "omniroute"},
		{repoName: "O360-AI-Memory", want: "o360-ai-memory"},
		{repoName: "O360_Stack_Lab", want: "o360-stack-lab"},
		{repoName: "countryside-off-road", want: "countryside-off-road"},
		{repoName: "...", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.repoName, func(t *testing.T) {
			t.Parallel()
			got, err := repositoryWorkspaceName(tc.repoName)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// fakeGitHubReposServer serves paginated /orgs/{org}/repos responses from a
// fixed in-memory repo list, mirroring the real API's per_page/page params.
func fakeGitHubReposServer(t *testing.T, repos []map[string]any) *httptest.Server {
	t.Helper()
	const perPage = 100
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		page := 1
		if p := r.URL.Query().Get("page"); p != "" && p != "1" {
			// Only page 1 and 2 are exercised by these tests.
			page = 2
		}
		start := (page - 1) * perPage
		end := start + perPage
		if start > len(repos) {
			start = len(repos)
		}
		if end > len(repos) {
			end = len(repos)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repos[start:end])
	}))
}

func TestFetchGitHubOrgRepositories_ExcludesArchivedAndDisabled(t *testing.T) {
	repos := []map[string]any{
		{"id": 1, "name": "OmniRoute", "full_name": "Observability360/OmniRoute", "private": true, "archived": false, "disabled": false, "default_branch": "main", "ssh_url": "git@github.com:Observability360/OmniRoute.git", "html_url": "https://github.com/Observability360/OmniRoute", "owner": map[string]any{"login": "Observability360"}},
		{"id": 2, "name": "old-archived-thing", "full_name": "Observability360/old-archived-thing", "private": true, "archived": true, "disabled": false, "owner": map[string]any{"login": "Observability360"}},
		{"id": 3, "name": "billing-disabled-repo", "full_name": "Observability360/billing-disabled-repo", "private": true, "archived": false, "disabled": true, "owner": map[string]any{"login": "Observability360"}},
	}
	srv := fakeGitHubReposServer(t, repos)
	defer srv.Close()

	original := githubAPIBaseURL
	githubAPIBaseURL = srv.URL
	defer func() { githubAPIBaseURL = original }()

	got, err := fetchGitHubOrgRepositories(t.Context(), "Observability360", "test-token")
	require.NoError(t, err)
	require.Len(t, got, 1, "archived and disabled repos must be excluded")
	assert.Equal(t, "OmniRoute", got[0].Name)
	assert.Equal(t, int64(1), got[0].ID)
	assert.True(t, got[0].Private)
	assert.Equal(t, "git@github.com:Observability360/OmniRoute.git", got[0].CloneURL)
}

func TestFetchGitHubOrgRepositories_Paginates(t *testing.T) {
	repos := make([]map[string]any, 0, 150)
	for i := 0; i < 150; i++ {
		repos = append(repos, map[string]any{
			"id": i, "name": "repo", "full_name": "Observability360/repo", "archived": false, "disabled": false,
			"owner": map[string]any{"login": "Observability360"},
		})
	}
	srv := fakeGitHubReposServer(t, repos)
	defer srv.Close()

	original := githubAPIBaseURL
	githubAPIBaseURL = srv.URL
	defer func() { githubAPIBaseURL = original }()

	got, err := fetchGitHubOrgRepositories(t.Context(), "Observability360", "test-token")
	require.NoError(t, err)
	assert.Len(t, got, 150, "must follow pagination past the first 100-item page")
}

func TestFetchGitHubOrgRepositories_UpstreamErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	original := githubAPIBaseURL
	githubAPIBaseURL = srv.URL
	defer func() { githubAPIBaseURL = original }()

	_, err := fetchGitHubOrgRepositories(t.Context(), "Observability360", "test-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestRepositoryCatalogCache_TTL(t *testing.T) {
	calls := 0
	repos := []map[string]any{
		{"id": 1, "name": "OmniRoute", "full_name": "Observability360/OmniRoute", "archived": false, "disabled": false, "owner": map[string]any{"login": "Observability360"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repos)
	}))
	defer srv.Close()

	original := githubAPIBaseURL
	githubAPIBaseURL = srv.URL
	defer func() { githubAPIBaseURL = original }()

	var cfg codersdk.RepositoryCatalogConfig
	_ = cfg.GitHubOrg.Set("Observability360")
	_ = cfg.GitHubToken.Set("test-token")

	var cache repositoryCatalogCache
	_, err := cache.get(t.Context(), cfg)
	require.NoError(t, err)
	_, err = cache.get(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "second call within TTL must reuse the cached result, not refetch")

	cache.fetchedAt = time.Now().Add(-repositoryCatalogCacheTTL - time.Second)
	_, err = cache.get(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "call after TTL expiry must refetch")
}
