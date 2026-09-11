package codersdk

import "github.com/google/uuid"

// Repository is a GitHub repository in the configured organization,
// returned by GET /api/v2/repository-catalog/repositories.
type Repository struct {
	ID            int64  `json:"id"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	// CloneURL is the SSH clone URL (git@github.com:owner/repo.git), matching
	// the format already stored in the docker-git template's git_repo
	// parameter for every existing repository workspace.
	CloneURL string `json:"clone_url"`
	HTMLURL  string `json:"html_url"`
	// WorkspaceID is set when the requesting user already owns a workspace
	// associated with this repository (nil otherwise). The frontend renders
	// "Ready" vs "Not created" from its presence, and "Attached" by comparing
	// it against the chat's currently attached workspace — a client-only bit
	// this field is not involved in.
	WorkspaceID *uuid.UUID `json:"workspace_id,omitempty"`
}

// RepositoryCatalogResponse is returned by GET /api/v2/repository-catalog/repositories.
type RepositoryCatalogResponse struct {
	Repositories []Repository `json:"repositories"`
}

// AttachRepositoryWorkspaceRequest is the body for
// POST /api/v2/repository-catalog/attach.
type AttachRepositoryWorkspaceRequest struct {
	Owner string `json:"owner" validate:"required"`
	Repo  string `json:"repo" validate:"required"`
}

// AttachRepositoryWorkspaceResponse is returned by
// POST /api/v2/repository-catalog/attach.
type AttachRepositoryWorkspaceResponse struct {
	Workspace Workspace `json:"workspace"`
	// Created is true when this call provisioned a brand new workspace;
	// false when an existing owned workspace for this repository was reused.
	Created bool `json:"created"`
}
