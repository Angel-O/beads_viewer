package ui

import (
	"context"
	"fmt"
	"os"
	"time"

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
	IssueSource         ChangeSource
	MetadataSources     []ChangeSource

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
	// AutoRefresh and the change sources are resolved by the composition root.
	// The UI only installs and drives the supplied neutral lifecycle services.
	AutoRefresh         bool
	SourceChangeSource  ChangeSource
	CatalogChangeSource ChangeSource
	// HubScopeMemberIDs bounds every Hub snapshot to the active named scope.
	// A nil loader preserves ordinary local loading semantics.
	HubScopeMemberIDs func(context.Context) ([]string, error)
	// InitialScope is the scope state resolved before Hub issue loading. A
	// non-nil snapshot with no Active scope keeps startup on the no-scope view.
	InitialScope *ScopeSnapshot
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
		IssueSource:             services.IssueSource,
		MetadataSources:         services.MetadataSources,
		SourceChangeSource:      services.SourceChangeSource,
		CatalogChangeSource:     services.CatalogChangeSource,
		LabelPredicate:          services.LabelPredicate,
		IssueRepositoryResolver: services.IssueRepositoryResolver,
		HubScopeMemberIDs:       services.HubScopeMemberIDs,
		SkipInitialRefresh:      services.InitialScope != nil && services.InitialScope.Active == nil,
	}
}

func runtimeIssuePaths(beadsPath string, services RuntimeServices) (string, string) {
	selectedIssuePath := services.SelectedIssuePath
	if selectedIssuePath == "" {
		selectedIssuePath = beadsPath
	}
	issueChangePath := services.IssueChangePath
	if issueChangePath == "" {
		issueChangePath = beadsPath
	}
	return selectedIssuePath, issueChangePath
}

func newRuntimeBackgroundWorker(beadsPath string, services RuntimeServices, backgroundModeRequested, force bool) (*BackgroundWorker, error) {
	selectedIssuePath, issueChangePath := runtimeIssuePaths(beadsPath, services)
	metadataChangePaths := services.MetadataChangePaths
	sourceChangeSource := services.SourceChangeSource
	catalogChangeSource := services.CatalogChangeSource
	catalogPath := services.CatalogPath
	if !services.AutoRefresh {
		sourceChangeSource = nil
		catalogChangeSource = nil
		catalogPath = ""
	}
	if !force && (issueChangePath == "" && len(metadataChangePaths) == 0 || !backgroundModeRequested && sourceChangeSource == nil && catalogChangeSource == nil) {
		return nil, nil
	}
	workerConfig := workerConfigForRuntime(beadsPath, services)
	workerConfig.SelectedIssuePath = selectedIssuePath
	workerConfig.IssueChangePath = issueChangePath
	workerConfig.MetadataChangePaths = metadataChangePaths
	workerConfig.CatalogPath = catalogPath
	workerConfig.SourceChangeSource = sourceChangeSource
	workerConfig.CatalogChangeSource = catalogChangeSource
	workerConfig.DebounceDelay = 200 * time.Millisecond
	return NewBackgroundWorker(workerConfig)
}

// SetRuntimeServices installs already-resolved services and owns their
// lifecycle adaptation; Model only invokes this composition hook.
func (m *Model) SetRuntimeServices(services RuntimeServices) {
	m.runtimeServices = services
	if services.SemanticDatasetPath != "" {
		m.semanticPath = services.SemanticDatasetPath
	}
	m.currentRepositoryID = services.CurrentRepositoryID
	// Presentation is an explicit composition decision. Resolver/capability
	// presence must not change the viewer's mode downstream.
	m.hubRepositoryMode = services.RepositoryPresentation
	if services.CatalogPath == "" || services.CatalogLoader == nil {
		m.refreshRepositoryPresentation()
		m.applyInitialRepositorySelection(services.InitialRepositorySelection)
		return
	}
	if err := m.reloadRepositoryCatalog(); err != nil {
		m.statusMsg = fmt.Sprintf("Repository catalog load failed: %v", err)
		m.statusIsError = true
	}
	m.refreshRepositoryPresentation()
	autoRefresh := services.AutoRefresh
	if m.backgroundWorker == nil && m.beadsPath != "" && autoRefresh {
		worker, err := newRuntimeBackgroundWorker(m.beadsPath, services, false, true)
		if err != nil {
			m.statusMsg = fmt.Sprintf("Repository catalog refresh unavailable: %v", err)
			m.statusIsError = true
		} else if worker != nil {
			m.backgroundWorker = worker
			m.snapshotInitPending = len(m.issues) == 0
		}
	} else if m.backgroundWorker != nil {
		m.backgroundWorker.UpdateRuntimeServices(services)
		if err := m.backgroundWorker.SetCatalogPath(services.CatalogPath, autoRefresh); err != nil {
			m.statusMsg = fmt.Sprintf("Repository catalog refresh unavailable: %v", err)
			m.statusIsError = true
		}
	}
	m.applyInitialRepositorySelection(services.InitialRepositorySelection)
}

func (m Model) catalogPath() string { return m.runtimeServices.CatalogPath }

func (m Model) runtimeHistoryProvider() *correlation.Provider {
	if m.runtimeServices.HistoryProvider != nil {
		return m.runtimeServices.HistoryProvider
	}
	workDir := m.workDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return correlation.NewGitProvider(workDir, resolveHistoryCorrelationPath(m.beadsPath, workDir))
}
