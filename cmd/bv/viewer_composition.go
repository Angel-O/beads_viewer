package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/internal/datasource"
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/hub"
	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

// viewerCompositionInput contains policy decisions already parsed by the CLI.
// It is deliberately a data-only boundary: flag and environment policy stays
// in cmd/bv, while consumers receive resolved services below.
type viewerCompositionInput struct {
	HistoryMode        string
	HubConfigPath      string
	ExplicitDBPath     string
	WorkspacePath      string
	AsOf               string
	WorkDir            string
	RobotMode          bool
	WrapperScope       string
	RefreshEnvironment string
}

// viewerComposition is the one resolved set of services shared by robot and
// TUI execution. No consumer needs to reconstruct history or Hub selection.
type viewerComposition struct {
	HubConfigPath          string
	UsesHubConfigStore     bool
	SelectedIssuePath      string
	SelectedIssueSource    datasource.DataSource
	HistoryProvider        *correlation.Provider
	CatalogLoader          func(string, []model.Issue) (repository.Catalog, error)
	HubScope               *hub.HubScope
	SemanticDatasetPath    string
	SemanticStorePath      string
	IssueChangePath        string
	MetadataChangePaths    []string
	RepositoryPresentation bool
	DefaultCurrentContext  string
	WorkspacePath          string
	AsOf                   string
	HubAutoRefresh         bool
}

func composeViewerServices(input viewerCompositionInput) (viewerComposition, error) {
	mode, configPath, err := resolveHistoryConfiguration(input.HistoryMode, input.HubConfigPath)
	if err != nil {
		return viewerComposition{}, err
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
	if usesHubStore {
		semanticStore, err = correlation.HubConfigStore(configPath)
		if err != nil {
			return viewerComposition{}, err
		}
	}
	var provider *correlation.Provider
	switch mode {
	case "off":
		provider = correlation.NewDisabledProvider()
	case "external":
		provider = correlation.NewExternalProvider(configPath)
	}

	selectedIssuePath := ""
	var selectedSource datasource.DataSource
	if input.WorkspacePath == "" && input.AsOf == "" && (mode == "git" || usesHubStore) {
		sourcePath := input.ExplicitDBPath
		if usesHubStore {
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
	scope, err := parseHubRobotScope(input.WrapperScope, configPath, usesHubStore, input.RobotMode)
	if err != nil {
		return viewerComposition{}, err
	}

	defaultCurrentContext := ""
	if mode != "off" {
		defaultCurrentContext = currentHubRepositoryContext(workDir, usesHubStore)
	}

	return viewerComposition{
		HubConfigPath:          configPath,
		UsesHubConfigStore:     usesHubStore,
		SelectedIssuePath:      selectedIssuePath,
		SelectedIssueSource:    selectedSource,
		HistoryProvider:        provider,
		CatalogLoader:          hub.LoadRepositoryCatalog,
		HubScope:               scope,
		SemanticDatasetPath:    semanticDataset,
		SemanticStorePath:      semanticStore,
		IssueChangePath:        selectedIssuePath,
		MetadataChangePaths:    compositionMetadataPaths(selectedIssuePath),
		RepositoryPresentation: usesHubStore,
		DefaultCurrentContext:  defaultCurrentContext,
		WorkspacePath:          input.WorkspacePath,
		AsOf:                   input.AsOf,
		HubAutoRefresh:         compositionHubAutoRefreshEnabled(input.RefreshEnvironment),
	}, nil
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
