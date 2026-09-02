package ui

import (
	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

// RuntimeServices are the already-resolved services supplied by the CLI.
// Model keeps no history-mode or Hub-selection policy; standalone callers may
// leave this zero-valued and receive local Git defaults.
type RuntimeServices struct {
	HistoryProvider        *correlation.Provider
	SelectedIssuePath      string
	IssueChangePath        string
	MetadataChangePaths    []string
	CatalogPath            string
	CatalogLoader          func(string, []model.Issue) (repositorypkg.Catalog, error)
	SemanticDatasetPath    string
	SemanticStorePath      string
	RepositoryPresentation bool
	DefaultRepositoryID    string
	ExternalHistory        bool
	HubAutoRefresh         bool
	RefreshResolved        bool
}
