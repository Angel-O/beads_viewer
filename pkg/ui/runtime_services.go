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

// IssueRepositoryResolver returns the repository IDs associated with an issue.
// Hub composition supplies the resolver; nil preserves local/workspace
// repository matching.
type IssueRepositoryResolver func(model.Issue) []string

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

	// IssueRepositoryResolver is supplied by Hub composition. Nil preserves
	// local/workspace repository matching.
	IssueRepositoryResolver IssueRepositoryResolver

	LabelPredicate      analysis.LabelPredicate
	SelectedIssuePath   string
	IssueChangePath     string
	MetadataChangePaths []string

	// CatalogPath identifies the source passed to CatalogLoader and its change
	// watcher; UI does not interpret or reopen that source.
	CatalogPath         string
	CatalogLoader       RepositoryMetadataProvider
	SemanticDatasetPath string
	SemanticStorePath   string
	// SemanticIndexDir is an optional already-resolved directory for the
	// provider/dimension-specific semantic index. Empty keeps local cache policy.
	SemanticIndexDir       string
	RepositoryPresentation bool

	// InitialRepositorySelection is applied once after the initial catalog is
	// available. Nil preserves the existing all-repositories default.
	InitialRepositorySelection *repositorypkg.Selection
	// CurrentRepositoryID controls presentation independently of selection.
	CurrentRepositoryID string
	ExternalHistory     bool
	HubAutoRefresh      bool
	RefreshResolved     bool
	// HubScopeMemberIDs bounds every Hub snapshot to the active named scope.
	// A nil loader preserves ordinary local loading semantics.
	HubScopeMemberIDs func(context.Context) ([]string, error)
	// InitialScope is the scope state resolved before Hub issue loading. A
	// non-nil snapshot with no Active scope keeps startup on the no-scope view.
	InitialScope    *ScopeSnapshot
	HubChangeSignal string
}

func workerConfigForRuntime(beadsPath string, services RuntimeServices) WorkerConfig {
	selectedIssuePath := services.SelectedIssuePath
	if selectedIssuePath == "" {
		selectedIssuePath = beadsPath
	}
	issueChangePath := services.IssueChangePath
	if issueChangePath == "" {
		issueChangePath = beadsPath
	}
	return WorkerConfig{
		BeadsPath:               beadsPath,
		SelectedIssuePath:       selectedIssuePath,
		IssueChangePath:         issueChangePath,
		MetadataChangePaths:     services.MetadataChangePaths,
		CatalogPath:             services.CatalogPath,
		CatalogLoader:           services.CatalogLoader,
		LabelPredicate:          services.LabelPredicate,
		IssueRepositoryResolver: services.IssueRepositoryResolver,
		HubScopeMemberIDs:       services.HubScopeMemberIDs,
		SkipInitialRefresh:      services.InitialScope != nil && services.InitialScope.Active == nil,
		HubChangeSignal:         services.HubChangeSignal,
	}
}
