package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dicklesworthstone/beads_viewer/pkg/correlation"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
)

func TestNewModelInstallsRuntimeServicesOnce(t *testing.T) {
	loads := 0
	m := NewModel([]model.Issue{{ID: "alpha", Status: model.StatusOpen, Labels: []string{"ctx:alpha"}}}, nil, "", RuntimeServices{
		CatalogPath: "resolved-catalog",
		CatalogLoader: func(string, []model.Issue) (repositorypkg.Catalog, error) {
			loads++
			return repositorypkg.Catalog{{ID: "ctx:alpha", Name: "alpha", Kind: repositorypkg.IdentityExact}}, nil
		},
		RepositoryPresentation: true,
	})
	defer m.Stop()
	if loads != 1 {
		t.Fatalf("runtime service catalog loads = %d, want one", loads)
	}
}

func TestSetCatalogPathPreservesInjectedCatalogChangeSource(t *testing.T) {
	source := &testChangeSource{changes: make(chan struct{}, 1)}
	worker, err := NewBackgroundWorker(WorkerConfig{CatalogChangeSource: source})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	if err := worker.SetCatalogPath("catalog", true); err != nil {
		t.Fatal(err)
	}
	if worker.catalogSource != source || worker.catalogWatcher != nil {
		t.Fatalf("catalog source = %v, fallback watcher = %v", worker.catalogSource, worker.catalogWatcher)
	}
}

func TestModelHubCatalogRespectsAutoRefreshOptOut(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "a")
	t.Setenv("BV_BACKGROUND_MODE", "1")
	m := NewModel(nil, nil, issuesPath)
	defer m.Stop()
	m.SetRuntimeServices(RuntimeServices{
		HistoryProvider: correlation.NewExternalProvider(nil), CatalogPath: configPath,
		CatalogLoader: func(string, []model.Issue) (repositorypkg.Catalog, error) {
			return repositorypkg.Catalog{{ID: "ctx:a", Name: "a", Path: "/a", Kind: repositorypkg.IdentityExact}}, nil
		},
		RepositoryPresentation: true, ExternalHistory: true, AutoRefresh: false,
	})
	if m.backgroundWorker == nil || m.backgroundWorker.catalogPath != configPath {
		t.Fatal("manual catalog refresh was not configured")
	}
	if m.backgroundWorker.catalogWatcher != nil || m.backgroundWorker.sourceWatcher != nil {
		t.Fatal("Hub auto-refresh opt-out left a Hub watcher enabled")
	}
}

func TestModelRuntimeServicesInstallCatalogLoaderOnExistingWorker(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	m := NewModel(nil, nil, "")
	m.backgroundWorker = worker
	loader := func(string, []model.Issue) (repositorypkg.Catalog, error) {
		return repositorypkg.Catalog{{ID: "ctx:injected"}}, nil
	}
	predicate := func(label string) bool { return label != "ctx:injected" }
	resolver := func(model.Issue) []string { return []string{"ctx:injected"} }
	members := func(context.Context) ([]string, error) { return []string{"one"}, nil }
	m.SetRuntimeServices(RuntimeServices{
		CatalogPath:             "catalog",
		CatalogLoader:           loader,
		LabelPredicate:          predicate,
		IssueRepositoryResolver: resolver,
		HubScopeMemberIDs:       members,
		AutoRefresh:             false,
		RepositoryPresentation:  true,
	})
	if worker.catalogLoader == nil {
		t.Fatal("existing worker lost the injected catalog loader")
	}
	if worker.labelPredicate == nil || worker.labelPredicate("ctx:injected") {
		t.Fatal("existing worker lost the injected label predicate")
	}
	if worker.issueRepositoryResolver == nil || worker.hubScopeMemberIDs == nil {
		t.Fatal("existing worker lost resolved runtime capabilities")
	}
	catalog, err := worker.catalogLoader("ignored", nil)
	if err != nil || len(catalog) != 1 || catalog[0].ID != "ctx:injected" {
		t.Fatalf("existing worker catalog loader = %#v, %v", catalog, err)
	}
}

func TestModelDirectHubModeEnablesConfigWatcher(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "a")
	t.Setenv("BV_BACKGROUND_MODE", "")
	m := NewModel(nil, nil, issuesPath)
	defer m.Stop()
	if m.backgroundWorker != nil || m.watcher == nil {
		t.Fatal("direct mode did not start with the ordinary file watcher")
	}
	m.SetRuntimeServices(RuntimeServices{
		HistoryProvider: correlation.NewExternalProvider(nil), CatalogPath: configPath,
		CatalogLoader: func(path string, _ []model.Issue) (repositorypkg.Catalog, error) {
			if hubCatalogMarker(path) == "b" {
				return repositorypkg.Catalog{{ID: "ctx:b", Name: "b", Path: "/b", Kind: repositorypkg.IdentityExact}}, nil
			}
			return repositorypkg.Catalog{{ID: "ctx:a", Name: "a", Path: "/a", Kind: repositorypkg.IdentityExact}}, nil
		},
		RepositoryPresentation: true, ExternalHistory: true, AutoRefresh: true,
	})
	if m.backgroundWorker == nil || m.backgroundWorker.catalogWatcher == nil || m.watcher == nil {
		t.Fatal("Hub provider did not retain the file watcher during worker transition")
	}
	if err := m.backgroundWorker.Start(); err != nil {
		t.Fatal(err)
	}
	m.backgroundWorker.TriggerRefresh()
	initial := waitForSnapshotReady(t, m.backgroundWorker.Messages())
	updated, _ := m.Update(initial)
	m = updated.(*Model)
	if m.watcher != nil {
		t.Fatal("fallback file watcher remained after the Hub worker produced a snapshot")
	}
	temporary := filepath.Join(directory, "hub.yaml.next")
	writeHubCatalogMarker(t, temporary, "b")
	if err := os.Rename(temporary, configPath); err != nil {
		t.Fatal(err)
	}
	ready := waitForCatalogReady(t, m.backgroundWorker.Messages())
	if len(ready.Catalog) != 1 || ready.Catalog[0].ID != "ctx:b" {
		t.Fatalf("atomic replacement catalog = %#v", ready.Catalog)
	}
}

func TestModelHubWorkerStartFailureRestoresFileWatcher(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "a")
	t.Setenv("BV_BACKGROUND_MODE", "")
	m := NewModel(nil, nil, issuesPath)
	defer m.Stop()
	m.SetRuntimeServices(RuntimeServices{
		HistoryProvider: correlation.NewExternalProvider(nil), CatalogPath: configPath,
		CatalogLoader: func(string, []model.Issue) (repositorypkg.Catalog, error) {
			return repositorypkg.Catalog{{ID: "ctx:a", Name: "a", Path: "/a", Kind: repositorypkg.IdentityExact}}, nil
		},
		RepositoryPresentation: true, ExternalHistory: true, AutoRefresh: true,
	})
	if m.backgroundWorker == nil || m.watcher == nil || !m.watcher.IsStarted() {
		t.Fatal("Hub transition did not retain a live fallback watcher")
	}
	updated, cmd := m.Update(SnapshotErrorMsg{Err: errors.New("start failed"), StartFailure: true})
	m = updated.(*Model)
	if m.backgroundWorker != nil || m.watcher == nil || !m.watcher.IsStarted() || cmd == nil {
		t.Fatalf("worker start failure did not restore file watching: worker=%v watcher=%v cmd=%v", m.backgroundWorker, m.watcher, cmd)
	}
}

func TestModelEmptyHubStartsWithRegisteredRepositories(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "empty")
	t.Setenv("BV_BACKGROUND_MODE", "")
	m := NewModel(nil, nil, issuesPath)
	defer m.Stop()
	m.SetRepositoryCatalogIssues(nil)
	m.SetRuntimeServices(RuntimeServices{
		HistoryProvider: correlation.NewExternalProvider(nil), CatalogPath: configPath,
		CatalogLoader: func(string, []model.Issue) (repositorypkg.Catalog, error) {
			return repositorypkg.Catalog{{ID: "ctx:empty", Name: "empty", Path: "/empty", Kind: repositorypkg.IdentityExact}}, nil
		},
		RepositoryPresentation: true, ExternalHistory: true, AutoRefresh: true,
	})
	if len(m.repositoryCatalog) != 1 || m.repositoryCatalog[0].ID != "ctx:empty" || m.repositoryCatalog[0].BeadCount != 0 {
		t.Fatalf("empty Hub catalog = %#v", m.repositoryCatalog)
	}
	if m.backgroundWorker == nil || !m.snapshotInitPending {
		t.Fatal("empty Hub did not remain active for background startup")
	}
}

func TestModelUsesInjectedCatalogCounts(t *testing.T) {
	m := NewModel([]model.Issue{{ID: "OPEN", Labels: []string{"ctx:a"}}}, nil, "")
	m.SetRuntimeServices(RuntimeServices{
		CatalogPath: "resolved-catalog",
		CatalogLoader: func(string, []model.Issue) (repositorypkg.Catalog, error) {
			return repositorypkg.Catalog{{ID: "ctx:a", Name: "a", Path: "/a", BeadCount: 2, Kind: repositorypkg.IdentityExact}}, nil
		},
		RepositoryPresentation: true,
	})
	if got := catalogEntry(m.repositoryCatalog, "ctx:a").BeadCount; got != 2 {
		t.Fatalf("injected catalog count = %d, want 2", got)
	}
}

func TestModelWorkspaceCatalogIgnoresHubCatalogMessages(t *testing.T) {
	m := &Model{
		workspaceMode: true,
		repositoryScopeController: repositoryScopeController{
			repositoryCatalog:   repositorypkg.Catalog{{ID: "api", Name: "api", Kind: repositorypkg.IdentityPrefix}},
			repositorySelection: repositorypkg.NewAllSelection(),
		},
	}
	updated, _ := m.Update(RepositoryCatalogReadyMsg{Generation: 3, Catalog: repositorypkg.Catalog{{ID: "ctx:api", Name: "api", Kind: repositorypkg.IdentityExact}}})
	m = updated.(*Model)
	if len(m.repositoryCatalog) != 1 || m.repositoryCatalog[0].ID != "api" || m.catalogGeneration != 0 {
		t.Fatalf("Hub catalog replaced workspace catalog: %#v", m.repositoryCatalog)
	}
	updated, _ = m.Update(RepositoryCatalogErrorMsg{Generation: 4, Err: errors.New("Hub config unavailable")})
	m = updated.(*Model)
	if m.statusMsg != "" || m.catalogGeneration != 0 {
		t.Fatalf("Hub catalog error leaked into workspace mode: status=%q generation=%d", m.statusMsg, m.catalogGeneration)
	}
}

func TestModelWorkspaceCatalogCountsRefreshWithBackgroundSnapshot(t *testing.T) {
	issues := []model.Issue{{ID: "api-1", Title: "One", Status: model.StatusOpen, IssueType: model.TypeTask}}
	m := NewModel(issues, nil, "")
	m.EnableWorkspaceMode(WorkspaceInfo{Enabled: true, RepoCount: 1, RepoPrefixes: []string{"api"}})
	snapshot := NewSnapshotBuilder([]model.Issue{
		{ID: "api-1", Title: "One", Status: model.StatusOpen, IssueType: model.TypeTask},
		{ID: "api-2", Title: "Two", Status: model.StatusOpen, IssueType: model.TypeTask},
	}).Build()
	updated, _ := m.Update(SnapshotReadyMsg{Snapshot: snapshot})
	m = updated.(*Model)
	if got := catalogEntry(m.repositoryCatalog, "api").BeadCount; got != 2 {
		t.Fatalf("workspace background count = %d, want 2", got)
	}
}

func TestBackgroundWorkerCatalogRefreshesIndependentlyAndRecovers(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task","labels":["ctx:a"]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "initial")
	catalogLoader := func(path string, _ []model.Issue) (repositorypkg.Catalog, error) {
		switch hubCatalogMarker(path) {
		case "changed":
			return repositorypkg.Catalog{
				{ID: "ctx:a", Name: "repo", Path: "/renamed/a/repo", BeadCount: 1, Kind: repositorypkg.IdentityExact},
				{ID: "ctx:new", Name: "new", Path: "/team/new", Kind: repositorypkg.IdentityExact},
			}, nil
		case "error":
			return nil, errors.New("catalog unavailable")
		case "recovered":
			return repositorypkg.Catalog{{ID: "ctx:recovered", Name: "recovered", Path: "/team/recovered", Kind: repositorypkg.IdentityExact}}, nil
		default:
			return repositorypkg.Catalog{
				{ID: "ctx:a", Name: "repo", Path: "/team/a/repo", BeadCount: 1, Kind: repositorypkg.IdentityExact},
				{ID: "ctx:zero", Name: "zero", Path: "/team/zero", Kind: repositorypkg.IdentityExact},
			}, nil
		}
	}
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, CatalogPath: configPath, CatalogLoader: catalogLoader, SourceRetryBase: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	worker.process()
	first := waitForCatalogReady(t, worker.Messages())
	if len(first.Catalog) != 2 || catalogEntry(first.Catalog, "ctx:a").BeadCount != 1 || catalogEntry(first.Catalog, "ctx:zero").BeadCount != 0 {
		t.Fatalf("initial catalog = %#v", first.Catalog)
	}
	initialHash := worker.LastHash()
	writeHubCatalogMarker(t, configPath, "changed")
	worker.markCatalogDirty()
	worker.process()
	changed := waitForCatalogReady(t, worker.Messages())
	if worker.LastHash() != initialHash {
		t.Fatal("catalog-only refresh changed issue snapshot hash")
	}
	if len(changed.Catalog) != 2 || catalogEntry(changed.Catalog, "ctx:a").Path != "/renamed/a/repo" || catalogEntry(changed.Catalog, "ctx:new").ID == "" {
		t.Fatalf("changed catalog = %#v", changed.Catalog)
	}
	writeHubCatalogMarker(t, configPath, "error")
	worker.markCatalogDirty()
	worker.process()
	waitForCatalogError(t, worker.Messages())
	if got := catalogEntry(worker.catalog, "ctx:a").Path; got != "/renamed/a/repo" {
		t.Fatalf("transient failure replaced last valid catalog: %q", got)
	}
	writeHubCatalogMarker(t, configPath, "recovered")
	worker.markCatalogDirty()
	worker.process()
	recovered := waitForCatalogReady(t, worker.Messages())
	if len(recovered.Catalog) != 1 || recovered.Catalog[0].ID != "ctx:recovered" {
		t.Fatalf("recovered catalog = %#v", recovered.Catalog)
	}
}

func TestBackgroundWorkerCatalogIdenticalRecoveryClearsModelError(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "initial")
	catalogLoader := func(path string, _ []model.Issue) (repositorypkg.Catalog, error) {
		if hubCatalogMarker(path) == "error" {
			return nil, errors.New("catalog unavailable")
		}
		return repositorypkg.Catalog{{ID: "ctx:a", Name: "a", Path: "/a", Kind: repositorypkg.IdentityExact}}, nil
	}
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, CatalogPath: configPath, CatalogLoader: catalogLoader, SourceRetryBase: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	worker.process()
	initial := waitForCatalogReady(t, worker.Messages())
	writeHubCatalogMarker(t, configPath, "error")
	worker.markCatalogDirty()
	worker.process()
	loadError := waitForCatalogError(t, worker.Messages())
	m := &Model{}
	updated, _ := m.Update(initial)
	m = updated.(*Model)
	updated, _ = m.Update(loadError)
	m = updated.(*Model)
	if !m.statusIsError {
		t.Fatal("catalog failure did not set model error state")
	}
	writeHubCatalogMarker(t, configPath, "initial")
	worker.markCatalogDirty()
	worker.process()
	recovered := waitForCatalogReady(t, worker.Messages())
	if !recovered.Recovered {
		t.Fatal("identical catalog recovery was not emitted explicitly")
	}
	updated, _ = m.Update(recovered)
	m = updated.(*Model)
	if m.statusIsError || m.statusMsg != "" {
		t.Fatalf("identical recovery left stale error: status=%q error=%v", m.statusMsg, m.statusIsError)
	}
}

func TestBackgroundWorkerPairsSnapshotAndCatalogInOneMessage(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task","labels":["ctx:a"]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "a")
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, CatalogPath: configPath, CatalogLoader: func(string, []model.Issue) (repositorypkg.Catalog, error) {
		return repositorypkg.Catalog{{ID: "ctx:a", Name: "a", Kind: repositorypkg.IdentityExact}}, nil
	}, MessageBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	worker.process()
	message := waitForSnapshotReady(t, worker.Messages())
	if !message.CatalogChanged || len(message.Catalog) != 1 || message.Catalog[0].ID != "ctx:a" {
		t.Fatalf("snapshot did not carry its catalog update: %#v", message)
	}
	select {
	case extra := <-worker.Messages():
		if _, ok := extra.(RepositoryCatalogReadyMsg); ok {
			t.Fatal("paired catalog was emitted as an evicting second message")
		}
	default:
	}
}

func TestBackgroundWorkerCatalogGenerationSuppressesStaleResult(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "a")
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, CatalogPath: configPath, CatalogLoader: func(string, []model.Issue) (repositorypkg.Catalog, error) {
		return repositorypkg.Catalog{{ID: "ctx:a", Name: "a", Kind: repositorypkg.IdentityExact}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	worker.catalogLoader = func(_ string, _ []model.Issue) (repositorypkg.Catalog, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return repositorypkg.Catalog{{ID: "ctx:stale", Name: "stale"}}, nil
		}
		return repositorypkg.Catalog{{ID: "ctx:fresh", Name: "fresh"}}, nil
	}
	go worker.process()
	<-started
	worker.markCatalogDirty()
	worker.TriggerRefresh()
	close(release)
	ready := waitForCatalogReady(t, worker.Messages())
	if ready.Generation != 1 || len(ready.Catalog) != 1 || ready.Catalog[0].ID != "ctx:fresh" {
		t.Fatalf("catalog result = %#v", ready)
	}
}

func TestBackgroundWorkerCatalogCountsCompleteSetForOpenOnlySnapshot(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	content := `{"id":"OPEN","title":"Open","status":"open","priority":1,"issue_type":"task"}` + "\n" +
		`{"id":"CLOSED","title":"Closed","status":"closed","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(issuesPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, CatalogPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	loadedCount := 0
	worker.catalogLoader = func(_ string, issues []model.Issue) (repositorypkg.Catalog, error) {
		loadedCount = len(issues)
		return nil, nil
	}
	_, contextlessCount, countReady, err := worker.buildRepositoryCatalog(&DataSnapshot{Issues: []model.Issue{{ID: "OPEN"}}, LoadedOpenOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if loadedCount != 2 {
		t.Fatalf("catalog issue count = %d, want complete set of 2", loadedCount)
	}
	if contextlessCount != 2 || !countReady {
		t.Fatalf("catalog counts = %d, ready=%v; want 2, true", contextlessCount, countReady)
	}
	worker.catalogLoader = func(_ string, _ []model.Issue) (repositorypkg.Catalog, error) {
		return nil, errors.New("catalog unavailable")
	}
	_, contextlessCount, countReady, err = worker.buildRepositoryCatalog(&DataSnapshot{Issues: []model.Issue{{ID: "OPEN"}}, LoadedOpenOnly: true})
	if err == nil || contextlessCount != 2 || !countReady {
		t.Fatalf("catalog failure counts = %d, ready=%v, err=%v", contextlessCount, countReady, err)
	}
}

func TestBackgroundWorkerContextlessCountUsesInjectedRepositoryResolver(t *testing.T) {
	issues := []model.Issue{{ID: "empty"}, {ID: "ordinary", Labels: []string{"kind:bug"}}, {ID: "unknown-context", Labels: []string{"ctx:unknown"}}, {ID: "registered", Labels: []string{"ctx:known"}}}
	worker, err := NewBackgroundWorker(WorkerConfig{
		CatalogPath: "catalog.yaml",
		IssueRepositoryResolver: func(issue model.Issue) []string {
			for _, label := range issue.Labels {
				if label == "ctx:unknown" || label == "ctx:known" {
					return []string{label}
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	worker.catalogLoader = func(_ string, _ []model.Issue) (repositorypkg.Catalog, error) {
		return repositorypkg.Catalog{{ID: "ctx:known", Kind: repositorypkg.IdentityExact}}, nil
	}
	_, count, ready, err := worker.buildRepositoryCatalog(&DataSnapshot{Issues: issues})
	if err != nil || !ready || count != 2 {
		t.Fatalf("successful catalog count = %d, ready=%v, err=%v; want 2, true, nil", count, ready, err)
	}
	worker.catalogLoader = func(_ string, _ []model.Issue) (repositorypkg.Catalog, error) {
		return nil, errors.New("catalog unavailable")
	}
	_, count, ready, err = worker.buildRepositoryCatalog(&DataSnapshot{Issues: issues})
	if err == nil || !ready || count != 2 {
		t.Fatalf("failed catalog count = %d, ready=%v, err=%v; want 2, true, error", count, ready, err)
	}
}

func TestBackgroundWorkerWatchesAtomicHubConfigReplacement(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeHubCatalogMarker(t, configPath, "a")
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, CatalogPath: configPath, CatalogLoader: func(path string, _ []model.Issue) (repositorypkg.Catalog, error) {
		if hubCatalogMarker(path) == "b" {
			return repositorypkg.Catalog{{ID: "ctx:b", Name: "b", Kind: repositorypkg.IdentityExact}}, nil
		}
		return repositorypkg.Catalog{{ID: "ctx:a", Name: "a", Kind: repositorypkg.IdentityExact}}, nil
	}, DebounceDelay: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	worker.TriggerRefresh()
	waitForCatalogReady(t, worker.Messages())
	temporary := filepath.Join(directory, "hub.yaml.next")
	writeHubCatalogMarker(t, temporary, "b")
	if err := os.Rename(temporary, configPath); err != nil {
		t.Fatal(err)
	}
	ready := waitForCatalogReady(t, worker.Messages())
	if len(ready.Catalog) != 1 || ready.Catalog[0].ID != "ctx:b" {
		t.Fatalf("atomic replacement catalog = %#v", ready.Catalog)
	}
}

// Marker files only trigger watcher transitions; catalog values stay literal
// in the injected callbacks above.
func writeHubCatalogMarker(t *testing.T, path, marker string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
}

func hubCatalogMarker(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}
