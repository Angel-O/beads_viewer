package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/internal/datasource"
	"github.com/Dicklesworthstone/beads_viewer/pkg/analysis"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/repository"
	"github.com/Dicklesworthstone/beads_viewer/pkg/ui"
	"github.com/Dicklesworthstone/beads_viewer/pkg/watcher"
)

// viewerCompositionInput contains policy decisions already parsed by the CLI.
// It is deliberately a data-only boundary: flag and environment policy stays
// in cmd/bv, while consumers receive resolved services below.
type viewerCompositionInput struct {
	HistoryMode     string
	HubConfigPath   string
	HistoryResolved bool
	ExplicitDBPath  string
	WorkspacePath   string
	AsOf            string
	WorkDir         string
	RobotMode       bool

	// HubMode is authoritative for repository-aware Hub capabilities. History
	// mode and config may still select an external history source in local mode.
	HubMode            bool
	RefreshEnvironment string
	WrapperScope       string
}

// viewerComposition is the one resolved set of services shared by robot and
// TUI execution. No consumer needs to reconstruct history or Hub selection.
type viewerComposition struct {
	HubConfigPath          string
	UsesHubConfigStore     bool
	HubMode                bool
	SelectedIssuePath      string
	SelectedIssueSource    datasource.DataSource
	HistoryProvider        *correlation.Provider
	CatalogLoader          func(string, []model.Issue) (repository.Catalog, error)
	LabelPredicate         analysis.LabelPredicate
	SemanticDatasetPath    string
	SemanticStorePath      string
	SemanticIndexDir       string
	IssueChangePath        string
	MetadataChangePaths    []string
	RepositoryPresentation bool
	WorkspacePath          string
	AsOf                   string
	AutoRefresh            bool
	SourceChangeSource     ui.ChangeSource
	CatalogChangeSource    ui.ChangeSource

	IssueRepositoryResolver    ui.IssueRepositoryResolver
	InitialRepositorySelection *repository.Selection
	CurrentRepositoryID        string
	// HubScopeSnapshot is the bounded active-scope loader shared by robot and
	// export paths. HubScopeMemberIDs adapts the same seam for TUI refreshes.
	HubScopeSnapshot  hubScopeSnapshotLoader
	HubScopeMemberIDs hubScopeMemberLoader
	// HubRobotFilter preserves wbv's context/contextless selection inside the
	// already bounded active-scope issue slice.
	HubRobotFilter *hub.HubScope
	ScopeServices  ui.ScopeServices
}

// runtimeServicesFor adapts the resolved composition to the neutral UI
// runtime boundary. The CLI resolves policy once; the TUI only receives the
// resulting services and paths.
func (c viewerComposition) runtimeServicesFor(datasetPath string, initialScope *ui.ScopeSnapshot) ui.RuntimeServices {
	if datasetPath == "" {
		datasetPath = c.SemanticDatasetPath
	}
	catalogPath := ""
	if c.HubMode {
		catalogPath = c.HubConfigPath
	}
	return ui.RuntimeServices{
		Scopes:                     c.ScopeServices,
		HistoryProvider:            c.HistoryProvider,
		LabelPredicate:             c.LabelPredicate,
		SelectedIssuePath:          c.SelectedIssuePath,
		IssueChangePath:            c.IssueChangePath,
		MetadataChangePaths:        c.MetadataChangePaths,
		CatalogPath:                catalogPath,
		CatalogLoader:              c.CatalogLoader,
		SemanticDatasetPath:        datasetPath,
		SemanticStorePath:          c.SemanticStorePath,
		SemanticIndexDir:           c.SemanticIndexDir,
		RepositoryPresentation:     c.RepositoryPresentation,
		ExternalHistory:            c.HistoryProvider.External(),
		AutoRefresh:                c.AutoRefresh,
		SourceChangeSource:         c.SourceChangeSource,
		CatalogChangeSource:        c.CatalogChangeSource,
		HubScopeMemberIDs:          c.HubScopeMemberIDs,
		InitialScope:               initialScope,
		IssueRepositoryResolver:    c.IssueRepositoryResolver,
		InitialRepositorySelection: c.InitialRepositorySelection,
		CurrentRepositoryID:        c.CurrentRepositoryID,
	}
}

func composeViewerServices(input viewerCompositionInput) (viewerComposition, error) {
	mode, configPath := input.HistoryMode, input.HubConfigPath
	var err error
	if !input.HistoryResolved {
		mode, configPath, err = resolveHistoryConfiguration(mode, configPath)
		if err != nil {
			return viewerComposition{}, err
		}
	}
	usesHubStore := configPath != "" && mode != "git"
	if usesHubStore && input.WorkspacePath != "" {
		return viewerComposition{}, fmt.Errorf("--workspace cannot be combined with the configured hub store; config.store is authoritative")
	}
	if usesHubStore && input.AsOf != "" {
		return viewerComposition{}, fmt.Errorf("--as-of cannot be combined with the configured hub store; config.store is authoritative")
	}

	workDir := input.WorkDir
	if strings.TrimSpace(workDir) == "" {
		workDir, err = os.Getwd()
		if err != nil {
			return viewerComposition{}, fmt.Errorf("getting working directory: %w", err)
		}
	}
	semanticStore := ""
	semanticIndexDir := ""
	if usesHubStore {
		semanticStore, err = hub.StorePath(configPath)
		if err != nil {
			return viewerComposition{}, err
		}
	}
	if input.HubMode && semanticStore != "" {
		semanticIndexDir = hub.SemanticCacheDir(hub.Paths{Store: semanticStore})
	}
	var provider *correlation.Provider
	switch mode {
	case "off":
		provider = correlation.NewDisabledProvider()
	case "external":
		provider = correlation.NewExternalProvider(hub.NewExternalHistorySource(configPath))
	}

	selectedIssuePath := ""
	var selectedSource datasource.DataSource
	if input.WorkspacePath == "" && input.AsOf == "" && (mode == "git" || usesHubStore || input.ExplicitDBPath != "") {
		sourcePath := input.ExplicitDBPath
		if usesHubStore && sourcePath == "" {
			sourcePath = semanticStore
		}
		selectedIssuePath, selectedSource = compositionIssueSource(workDir, sourcePath)
	}

	if provider == nil {
		historyIssuePath := ""
		if selectedIssuePath != "" {
			historyIssuePath = compositionJSONLPath(selectedIssuePath)
		}
		provider = correlation.NewGitProvider(workDir, historyIssuePath)
	}

	semanticDataset := selectedIssuePath
	if input.AsOf != "" {
		semanticDataset = semanticAsOfDatasetPath(workDir)
	} else if input.WorkspacePath != "" {
		semanticDataset = input.WorkspacePath
	}
	var hubScopeMemberIDs hubScopeMemberLoader
	var hubScopeSnapshot hubScopeSnapshotLoader
	var scopeServices ui.ScopeServices
	if input.HubMode && !input.RobotMode {
		defaultPaths, pathErr := hub.DefaultPaths()
		if pathErr != nil {
			return viewerComposition{}, fmt.Errorf("resolving wbd default Hub store: %w", pathErr)
		}
		if filepath.Clean(semanticStore) != filepath.Clean(defaultPaths.Store) {
			return viewerComposition{}, fmt.Errorf("configured Viewer Hub store %q does not match wbd default Hub store %q; active scope loading is unavailable", semanticStore, defaultPaths.Store)
		}
		scopeServices = newHubScopeServices(workDir)
	}
	if input.HubMode {
		hubScopeSnapshot, err = hubScopeSnapshotLoaderForStore(true, workDir)
		if err != nil {
			return viewerComposition{}, err
		}
		hubScopeMemberIDs = func(ctx context.Context) ([]string, error) {
			snapshot, loadErr := hubScopeSnapshot(ctx)
			if loadErr != nil {
				return nil, loadErr
			}
			return snapshot.MemberIDs, nil
		}
	}
	var hubRobotFilter *hub.HubScope
	if input.HubMode && input.RobotMode {
		hubRobotFilter, err = decodeHubRobotFilter(input.WrapperScope, configPath)
		if err != nil {
			return viewerComposition{}, err
		}
	}

	var initialRepositorySelection *repository.Selection
	currentRepositoryID := ""
	if input.HubMode {
		currentRepositoryID = currentHubRepositoryContext(workDir, true)
		if currentRepositoryID != "" {
			selection, selectionErr := repository.NewSelectedSelection([]string{currentRepositoryID})
			if selectionErr != nil {
				return viewerComposition{}, selectionErr
			}
			initialRepositorySelection = &selection
		}
	}
	var labelPredicate analysis.LabelPredicate
	var catalogLoader func(string, []model.Issue) (repository.Catalog, error)
	var issueRepositoryResolver ui.IssueRepositoryResolver
	if input.HubMode {
		catalogLoader = hub.LoadRepositoryCatalog
		issueRepositoryResolver = func(issue model.Issue) []string {
			return hub.Contexts(issue.Labels)
		}
		labelPredicate = hub.AdmitLabel
	}
	hubAutoRefresh := false
	hubChangeSignal := ""
	var sourceChangeSource ui.ChangeSource
	var catalogChangeSource ui.ChangeSource
	if input.HubMode {
		hubAutoRefresh = compositionHubAutoRefreshEnabled(input.RefreshEnvironment)
		hubChangeSignal = hubChangeSignalPath(semanticStore)
		if hubAutoRefresh {
			sourceChangeSource, err = compositionChangeSource(hubChangeSignal)
			if err != nil {
				return viewerComposition{}, err
			}
			catalogChangeSource, err = compositionChangeSource(configPath)
			if err != nil {
				return viewerComposition{}, err
			}
		}
	}

	return viewerComposition{
		HubConfigPath:          configPath,
		UsesHubConfigStore:     usesHubStore,
		HubMode:                input.HubMode,
		SelectedIssuePath:      selectedIssuePath,
		SelectedIssueSource:    selectedSource,
		HistoryProvider:        provider,
		CatalogLoader:          catalogLoader,
		LabelPredicate:         labelPredicate,
		SemanticDatasetPath:    semanticDataset,
		SemanticStorePath:      semanticStore,
		SemanticIndexDir:       semanticIndexDir,
		IssueChangePath:        selectedIssuePath,
		MetadataChangePaths:    compositionMetadataPaths(selectedIssuePath),
		RepositoryPresentation: input.HubMode,
		WorkspacePath:          input.WorkspacePath,
		AsOf:                   input.AsOf,
		AutoRefresh:            hubAutoRefresh,
		SourceChangeSource:     sourceChangeSource,
		CatalogChangeSource:    catalogChangeSource,
		HubScopeSnapshot:       hubScopeSnapshot,
		HubScopeMemberIDs:      hubScopeMemberIDs,
		HubRobotFilter:         hubRobotFilter,
		ScopeServices:          scopeServices,

		IssueRepositoryResolver:    issueRepositoryResolver,
		InitialRepositorySelection: initialRepositorySelection,
		CurrentRepositoryID:        currentRepositoryID,
	}, nil
}

func compositionChangeSource(path string) (ui.ChangeSource, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	return watcher.NewWatcher(path,
		watcher.WithDebounceDuration(200*time.Millisecond),
		watcher.WithContentCheck(true),
	)
}

func decodeHubRobotFilter(raw, configPath string) (*hub.HubScope, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var selection hub.HubScope
	if err := decoder.Decode(&selection); err != nil {
		return nil, fmt.Errorf("decoding wbv Hub selection: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decoding wbv Hub selection: multiple JSON values")
		}
		return nil, fmt.Errorf("decoding wbv Hub selection: %w", err)
	}
	if err := selection.Validate(); err != nil {
		return nil, fmt.Errorf("invalid wbv Hub selection: %w", err)
	}
	if selection.Mode == hub.HubScopeSelectedContexts {
		config, err := hub.Resolve(configPath)
		if err != nil {
			return nil, fmt.Errorf("loading registered Hub contexts: %w", err)
		}
		for _, contextID := range selection.Contexts {
			if _, registered := config.Repositories[contextID]; !registered {
				return nil, fmt.Errorf("Hub context is not registered: %s", contextID)
			}
		}
	}
	return &selection, nil
}

func hubScopeSnapshotLoaderForStore(usesHubStore bool, workDir string) (hubScopeSnapshotLoader, error) {
	if !usesHubStore {
		return nil, nil
	}
	if _, err := exec.LookPath("wbd"); err != nil {
		return nil, fmt.Errorf("active Hub scope loading requires wbd: %w", err)
	}
	return newHubScopeSnapshotLoader(workDir), nil
}

func hubChangeSignalPath(store string) string {
	if strings.TrimSpace(store) == "" {
		return ""
	}
	return hub.ChangeSignalPath(hub.Paths{Store: store})
}

func compositionHubAutoRefreshEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func compositionIssueSource(workDir, explicitDB string) (string, datasource.DataSource) {
	path := strings.TrimSpace(explicitDB)
	if path != "" {
		if absolute, err := filepath.Abs(path); err == nil {
			path = absolute
		}
	}
	if path == "" {
		path = strings.TrimSpace(os.Getenv(loader.BeadsDBEnvVar))
	}
	if path == "" {
		path = strings.TrimSpace(os.Getenv(loader.BeadsDirEnvVar))
	}
	if path != "" {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			sources, err := datasource.DiscoverSources(datasource.DiscoveryOptions{
				BeadsDir: path, RepoPath: workDir, SkipWorktreeSources: true,
			})
			if err == nil && len(sources) > 0 {
				selected := sources[0]
				return selected.Path, selected
			}
		} else if source, ok, err := datasource.SourceFromFile(path); err == nil && ok {
			return source.Path, source
		}
		if path != "" {
			return path, datasource.DataSource{Path: path}
		}
	}

	beadsDir, err := loader.GetBeadsDir(workDir)
	if err != nil {
		return "", datasource.DataSource{}
	}
	sources, err := datasource.DiscoverSources(datasource.DiscoveryOptions{BeadsDir: beadsDir, RepoPath: workDir})
	if err != nil || len(sources) == 0 {
		return "", datasource.DataSource{}
	}
	selected := sources[0]
	return selected.Path, selected
}

func compositionJSONLPath(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return path
	}
	if sibling, err := loader.FindJSONLPath(filepath.Dir(path)); err == nil && sibling != "" {
		return sibling
	}
	return path
}

func compositionMetadataPaths(issuePath string) []string {
	if issuePath == "" {
		return nil
	}
	return []string{filepath.Join(filepath.Dir(issuePath), "metadata.jsonl")}
}
