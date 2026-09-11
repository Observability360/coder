package coderd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/coder/coder/v2/coderd/audit"
	"github.com/coder/coder/v2/coderd/database"
	"github.com/coder/coder/v2/coderd/httpapi"
	"github.com/coder/coder/v2/coderd/httpmw"
	"github.com/coder/coder/v2/codersdk"
)

// repositoryCatalogCacheTTL bounds how long a listed repository catalog is
// reused before refetching from GitHub. The picker only needs to be
// reasonably fresh (new repos, renames, archival) — not real-time — and this
// keeps "opening the menu" cheap even for a busy team.
const repositoryCatalogCacheTTL = 30 * time.Second

// repositoryCatalogCache is a single process-wide cache: the catalog is the
// same for every user (same org, same server-side token), so there is
// nothing to key it by.
type repositoryCatalogCache struct {
	mu        sync.Mutex
	repos     []codersdk.Repository
	fetchedAt time.Time
}

func (c *repositoryCatalogCache) get(ctx context.Context, cfg codersdk.RepositoryCatalogConfig) ([]codersdk.Repository, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.repos != nil && time.Since(c.fetchedAt) < repositoryCatalogCacheTTL {
		return c.repos, nil
	}
	repos, err := fetchGitHubOrgRepositories(ctx, cfg.GitHubOrg.Value(), cfg.GitHubToken.Value())
	if err != nil {
		return nil, err
	}
	c.repos = repos
	c.fetchedAt = time.Now()
	return repos, nil
}

// attachLocks serializes concurrent attach requests for the same
// (owner, repository) pair — a double-click or a retried request must reuse
// the workspace the first request created rather than racing a second one
// into existence. Process-local only: sufficient because this deployment
// runs a single coderd instance and the lock is held only for the duration
// of one find-or-create call.
var attachLocks sync.Map // map[string]*sync.Mutex

func attachLockFor(key string) *sync.Mutex {
	l, _ := attachLocks.LoadOrStore(key, &sync.Mutex{})
	return l.(*sync.Mutex)
}

func repositoryCatalogEnabled(cfg codersdk.RepositoryCatalogConfig) bool {
	return cfg.GitHubOrg.Value() != "" && cfg.GitHubToken.Value() != ""
}

// @Summary List repository catalog
// @Description Lists the configured GitHub organization's accessible,
// @Description non-archived repositories, annotated with the requesting
// @Description user's existing workspace (if any) for each.
// @ID list-repository-catalog
// @Security CoderSessionToken
// @Produce json
// @Tags Repositories
// @Success 200 {object} codersdk.RepositoryCatalogResponse
// @Failure 404 {object} codersdk.Response "Repository catalog is not configured on this deployment"
// @Failure 502 {object} codersdk.Response
// @Router /api/v2/repository-catalog/repositories [get]
func (api *API) repositoryCatalogRepositories(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := api.DeploymentValues.RepositoryCatalog
	apiKey := httpmw.APIKey(r)

	if !repositoryCatalogEnabled(cfg) {
		httpapi.Write(ctx, rw, http.StatusNotFound, codersdk.Response{
			Message: "Repository catalog is not configured on this deployment.",
		})
		return
	}

	repos, err := api.repositoryCatalogCache.get(ctx, cfg)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusBadGateway, codersdk.Response{
			Message: "Failed to list repositories from GitHub.",
			Detail:  err.Error(),
		})
		return
	}

	existing, err := api.ownedRepositoryWorkspaces(ctx, apiKey.UserID, cfg)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to look up existing repository workspaces.",
			Detail:  err.Error(),
		})
		return
	}

	// Copy before annotating: the cache is shared across users and must
	// never be mutated with one user's workspace associations.
	annotated := make([]codersdk.Repository, len(repos))
	for i, repo := range repos {
		annotated[i] = repo
		if ws, ok := existing[strings.ToLower(repo.FullName)]; ok {
			id := ws.ID
			annotated[i].WorkspaceID = &id
		}
	}

	httpapi.Write(ctx, rw, http.StatusOK, codersdk.RepositoryCatalogResponse{
		Repositories: annotated,
	})
}

// @Summary Attach (find-or-create) a repository workspace
// @Description Finds the requesting user's existing workspace associated
// @Description with the given repository, or lazily creates one from the
// @Description configured template. Idempotent: concurrent calls for the
// @Description same (owner, repository) never create more than one
// @Description workspace.
// @ID attach-repository-workspace
// @Security CoderSessionToken
// @Accept json
// @Produce json
// @Tags Repositories
// @Param request body codersdk.AttachRepositoryWorkspaceRequest true "Attach repository workspace request"
// @Success 200 {object} codersdk.AttachRepositoryWorkspaceResponse
// @Failure 404 {object} codersdk.Response "Repository catalog is not configured on this deployment"
// @Failure 502 {object} codersdk.Response
// @Router /api/v2/repository-catalog/attach [post]
func (api *API) repositoryCatalogAttach(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := api.DeploymentValues.RepositoryCatalog
	apiKey := httpmw.APIKey(r)

	if !repositoryCatalogEnabled(cfg) {
		httpapi.Write(ctx, rw, http.StatusNotFound, codersdk.Response{
			Message: "Repository catalog is not configured on this deployment.",
		})
		return
	}

	var req codersdk.AttachRepositoryWorkspaceRequest
	if !httpapi.Read(ctx, rw, r, &req) {
		return
	}

	// Only repositories the configured catalog actually contains may be
	// attached — never let a client request an arbitrary owner/repo pair
	// and have this endpoint clone it regardless of org membership.
	repos, err := api.repositoryCatalogCache.get(ctx, cfg)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusBadGateway, codersdk.Response{
			Message: "Failed to list repositories from GitHub.",
			Detail:  err.Error(),
		})
		return
	}
	var target *codersdk.Repository
	for i := range repos {
		if strings.EqualFold(repos[i].Owner, req.Owner) && strings.EqualFold(repos[i].Name, req.Repo) {
			target = &repos[i]
			break
		}
	}
	if target == nil {
		httpapi.Write(ctx, rw, http.StatusForbidden, codersdk.Response{
			Message: fmt.Sprintf("Repository %s/%s is not in the accessible catalog.", req.Owner, req.Repo),
		})
		return
	}

	lockKey := apiKey.UserID.String() + "/" + strings.ToLower(target.FullName)
	lock := attachLockFor(lockKey)
	lock.Lock()
	defer lock.Unlock()

	// Re-check for an existing workspace *inside* the lock: another request
	// for the same (user, repo) may have created one while we were waiting.
	existing, err := api.ownedRepositoryWorkspaces(ctx, apiKey.UserID, cfg)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to look up existing repository workspaces.",
			Detail:  err.Error(),
		})
		return
	}
	if ws, ok := existing[strings.ToLower(target.FullName)]; ok {
		full, err := api.fetchWorkspaceForResponse(ctx, apiKey.UserID, ws.ID)
		if err != nil {
			httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
				Message: "Failed to load existing workspace.",
				Detail:  err.Error(),
			})
			return
		}
		httpapi.Write(ctx, rw, http.StatusOK, codersdk.AttachRepositoryWorkspaceResponse{
			Workspace: full,
			Created:   false,
		})
		return
	}

	defaultOrg, err := api.Database.GetDefaultOrganization(ctx)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to resolve the default organization.",
			Detail:  err.Error(),
		})
		return
	}
	template, err := api.Database.GetTemplateByOrganizationAndName(ctx, database.GetTemplateByOrganizationAndNameParams{
		OrganizationID: defaultOrg.ID,
		Name:           cfg.TemplateName.Value(),
	})
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: fmt.Sprintf("Repository catalog template %q was not found.", cfg.TemplateName.Value()),
			Detail:  err.Error(),
		})
		return
	}

	user, err := api.Database.GetUserByID(ctx, apiKey.UserID)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: "Failed to load current user.",
			Detail:  err.Error(),
		})
		return
	}

	owner := workspaceOwner{
		ID:        user.ID,
		Username:  user.Username,
		AvatarURL: user.AvatarURL,
	}

	name, err := repositoryWorkspaceName(target.Name)
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusBadRequest, codersdk.Response{
			Message: err.Error(),
		})
		return
	}
	// A prior attempt for this exact repo may have left behind a workspace
	// under this deterministic name that our git_repo-based lookup above
	// did not classify as "this repo" (e.g. the template's git_repo default
	// changed). Fall back to a suffixed name rather than fail the create.
	name = uniqueWorkspaceName(ctx, api.Database, owner.ID, name)

	createReq := codersdk.CreateWorkspaceRequest{
		TemplateID: template.ID,
		Name:       name,
		RichParameterValues: []codersdk.WorkspaceBuildParameter{
			{Name: cfg.GitRepoParameterName.Value(), Value: target.CloneURL},
		},
	}

	auditor := api.Auditor.Load()
	aReq, commitAudit := audit.InitRequest[database.WorkspaceTable](rw, &audit.RequestParams{
		Audit:   *auditor,
		Log:     api.Logger,
		Request: r,
		Action:  database.AuditActionCreate,
		AdditionalFields: audit.AdditionalFields{
			WorkspaceOwner: owner.Username,
		},
	})
	defer commitAudit()

	w, err := createWorkspace(ctx, aReq, apiKey.UserID, api, owner, createReq, &createWorkspaceOptions{
		remoteAddr: r.RemoteAddr,
	})
	if err != nil {
		httpapi.Write(ctx, rw, http.StatusInternalServerError, codersdk.Response{
			Message: fmt.Sprintf("Failed to create a workspace for %s.", target.FullName),
			Detail:  err.Error(),
		})
		return
	}

	httpapi.Write(ctx, rw, http.StatusOK, codersdk.AttachRepositoryWorkspaceResponse{
		Workspace: w,
		Created:   true,
	})
}

// fetchWorkspaceForResponse loads a single workspace by ID through the same
// conversion path GET /api/v2/workspaces/{workspace} uses.
func (api *API) fetchWorkspaceForResponse(ctx context.Context, requesterID uuid.UUID, id uuid.UUID) (codersdk.Workspace, error) {
	workspace, err := api.Database.GetWorkspaceByID(ctx, id)
	if err != nil {
		return codersdk.Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	data, err := api.workspaceData(ctx, []database.Workspace{workspace}, allWorkspaceRelated())
	if err != nil {
		return codersdk.Workspace{}, fmt.Errorf("load workspace data: %w", err)
	}
	if len(data.templates) == 0 {
		return codersdk.Workspace{}, fmt.Errorf("workspace template not found")
	}
	appStatus := codersdk.WorkspaceAppStatus{}
	if len(data.appStatuses) > 0 {
		appStatus = data.appStatuses[0]
	}
	return convertWorkspace(
		ctx,
		api.Logger,
		requesterID,
		workspace,
		data.builds[0],
		data.templates[0],
		api.AllowWorkspaceRenames,
		appStatus,
	)
}

// ownedRepositoryWorkspaces returns, for every non-deleted workspace the
// given user owns on the configured template, a map from the lowercased
// "owner/repo" (parsed from the template's git-clone parameter) to that
// workspace row. This is the workspace<->repository identity: no new table
// or parameter is introduced, it is derived entirely from the existing
// git_repo build parameter every repository workspace (manually created or
// lazily created by this feature) already carries.
func (api *API) ownedRepositoryWorkspaces(ctx context.Context, ownerID uuid.UUID, cfg codersdk.RepositoryCatalogConfig) (map[string]database.Workspace, error) {
	defaultOrg, err := api.Database.GetDefaultOrganization(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve default organization: %w", err)
	}
	template, err := api.Database.GetTemplateByOrganizationAndName(ctx, database.GetTemplateByOrganizationAndNameParams{
		OrganizationID: defaultOrg.ID,
		Name:           cfg.TemplateName.Value(),
	})
	if err != nil {
		// No template yet (fresh deployment) — no existing workspaces
		// can possibly reference it.
		return map[string]database.Workspace{}, nil
	}

	rows, err := api.Database.GetWorkspaces(ctx, database.GetWorkspacesParams{
		OwnerID:     ownerID,
		TemplateIDs: []uuid.UUID{template.ID},
		Deleted:     false,
	})
	if err != nil {
		return nil, fmt.Errorf("list owned workspaces: %w", err)
	}

	result := make(map[string]database.Workspace, len(rows))
	for _, row := range rows {
		workspace, err := api.Database.GetWorkspaceByID(ctx, row.ID)
		if err != nil {
			continue
		}
		build, err := api.Database.GetLatestWorkspaceBuildByWorkspaceID(ctx, workspace.ID)
		if err != nil {
			continue
		}
		params, err := api.Database.GetWorkspaceBuildParameters(ctx, build.ID)
		if err != nil {
			continue
		}
		for _, p := range params {
			if p.Name != cfg.GitRepoParameterName.Value() {
				continue
			}
			owner, repo, ok := parseGitCloneURL(p.Value)
			if !ok {
				continue
			}
			result[strings.ToLower(owner+"/"+repo)] = workspace
		}
	}
	return result, nil
}

// parseGitCloneURL extracts (owner, repo) from either the SSH form
// (git@github.com:owner/repo.git) or the HTTPS form
// (https://github.com/owner/repo(.git)) of a GitHub clone URL.
func parseGitCloneURL(raw string) (owner string, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ".git")
	var rest string
	switch {
	case strings.HasPrefix(raw, "git@github.com:"):
		rest = strings.TrimPrefix(raw, "git@github.com:")
	case strings.HasPrefix(raw, "https://github.com/"):
		rest = strings.TrimPrefix(raw, "https://github.com/")
	case strings.HasPrefix(raw, "ssh://git@github.com/"):
		rest = strings.TrimPrefix(raw, "ssh://git@github.com/")
	default:
		return "", "", false
	}
	parts := strings.SplitN(strings.Trim(rest, "/"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// repositoryWorkspaceName derives a Coder-legal workspace name from a
// repository name. Workspace names are unique per (owner_id, lower(name)),
// not globally, so the repository's own name is sufficient — Luis and
// Gumiero can each independently own a workspace named "omniroute".
func repositoryWorkspaceName(repoName string) (string, error) {
	name := strings.ToLower(repoName)
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '_' || r == '.':
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "", fmt.Errorf("repository name %q has no valid workspace-name characters", repoName)
	}
	return result, nil
}

// uniqueWorkspaceName appends a numeric suffix if the deterministic name is
// already taken by an unrelated workspace this owner has (e.g. a previous
// manual workspace that happens to share the name but was never associated
// with this repository).
func uniqueWorkspaceName(ctx context.Context, db database.Store, ownerID uuid.UUID, base string) string {
	for attempt := 0; attempt < 20; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", base, attempt+1)
		}
		_, err := db.GetWorkspaceByOwnerIDAndName(ctx, database.GetWorkspaceByOwnerIDAndNameParams{
			OwnerID: ownerID,
			Name:    candidate,
		})
		if err != nil {
			// Not found (or any other lookup error) — treat as available;
			// createWorkspace's own validation is the real backstop.
			return candidate
		}
	}
	return base
}

// githubAPIBaseURL is a test seam: unit tests point this at an httptest
// server instead of the real GitHub API.
var githubAPIBaseURL = "https://api.github.com"

// fetchGitHubOrgRepositories lists every repository the configured token can
// see in the given organization, excluding archived and disabled
// repositories. Uses the plain GitHub REST API (no scraping), paginated.
func fetchGitHubOrgRepositories(ctx context.Context, org string, token string) ([]codersdk.Repository, error) {
	type ghRepo struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		Private       bool   `json:"private"`
		Archived      bool   `json:"archived"`
		Disabled      bool   `json:"disabled"`
		DefaultBranch string `json:"default_branch"`
		SSHURL        string `json:"ssh_url"`
		HTMLURL       string `json:"html_url"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}

	var all []codersdk.Repository
	client := &http.Client{Timeout: 15 * time.Second}
	for pageNum := 1; ; pageNum++ {
		url := fmt.Sprintf("%s/orgs/%s/repos?type=all&per_page=100&page=%d", githubAPIBaseURL, org, pageNum)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "coder-repository-catalog")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request github repos: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read github response: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			detail := string(body)
			if len(detail) > 500 {
				detail = detail[:500]
			}
			return nil, fmt.Errorf("github returned status %d: %s", resp.StatusCode, detail)
		}

		var repos []ghRepo
		if err := json.Unmarshal(body, &repos); err != nil {
			return nil, fmt.Errorf("decode github response: %w", err)
		}
		if len(repos) == 0 {
			break
		}
		for _, r := range repos {
			if r.Archived || r.Disabled {
				continue
			}
			all = append(all, codersdk.Repository{
				ID:            r.ID,
				Owner:         r.Owner.Login,
				Name:          r.Name,
				FullName:      r.FullName,
				Private:       r.Private,
				DefaultBranch: r.DefaultBranch,
				CloneURL:      r.SSHURL,
				HTMLURL:       r.HTMLURL,
			})
		}
		if len(repos) < 100 {
			break
		}
	}
	return all, nil
}
