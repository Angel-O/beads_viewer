package ui

import (
	"context"

	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

// RepositoryMetadataProvider loads presentation metadata for the complete
// issue universe. The Hub implementation is kept at this adapter boundary;
// the UI worker only knows this neutral function type.
type RepositoryMetadataProvider func(string, []model.Issue) (repositorypkg.Catalog, error)

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
	// Scopes supplies the explicit named-scope control plane and Global issues.
	// It is nil for local/standalone Viewer construction.
	Scopes          ScopeServices
	HistoryProvider *correlation.Provider
	// RepositoryCatalog is resolved by composition. A nil catalog preserves
	// local/standalone zero-value behavior.
	RepositoryCatalog   repositorypkg.Catalog
	LabelPredicate      analysis.LabelPredicate
	SelectedIssuePath   string
	IssueChangePath     string
	MetadataChangePaths []string
	// CatalogPath identifies the source passed to CatalogLoader and its change
	// watcher; UI does not interpret or reopen that source.
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
	// InitialScope is the scope state resolved before Hub issue loading. A
	// non-nil snapshot with no Active scope keeps startup on the no-scope view.
	InitialScope    *ScopeSnapshot
	HubChangeSignal string
}
