package ui

import (
	"context"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

// RepositoryMetadataProvider loads presentation metadata for the complete
// issue universe. The Hub implementation is kept at this adapter boundary;
// the UI worker only knows this neutral function type.
type RepositoryMetadataProvider func(string, []model.Issue) (repositorypkg.Catalog, error)

func defaultRepositoryMetadataProvider(path string, issues []model.Issue) (repositorypkg.Catalog, error) {
	return hub.LoadRepositoryCatalog(path, issues)
}

// ChangeSource is the small lifecycle and notification contract consumed by
// BackgroundWorker. watcher.Watcher satisfies it without becoming a worker
// policy dependency.
type ChangeSource interface {
	Start() error
	Stop()
	Changed() <-chan struct{}
	Path() string
}

// RuntimeServices are the already-resolved services supplied by the CLI.
// Model keeps no history-mode or Hub-selection policy; standalone callers may
// leave this zero-valued and receive local Git defaults.
type RuntimeServices struct {
	// Scopes supplies the explicit named-scope control plane and global backlog.
	// It is nil for local/standalone Viewer construction.
	Scopes                 ScopeServices
	HistoryProvider        *correlation.Provider
	SelectedIssuePath      string
	IssueChangePath        string
	MetadataChangePaths    []string
	CatalogPath            string
	CatalogLoader          RepositoryMetadataProvider
	SemanticDatasetPath    string
	SemanticStorePath      string
	RepositoryPresentation bool
	DefaultRepositoryID    string
	ExternalHistory        bool
	HubAutoRefresh         bool
	RefreshResolved        bool
	// HubScopeMemberIDs bounds every Hub snapshot to the active named scope.
	// A nil loader preserves ordinary local loading semantics.
	HubScopeMemberIDs func(context.Context) ([]string, error)
	HubChangeSignal   string
}
