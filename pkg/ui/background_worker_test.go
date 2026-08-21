package ui

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Dicklesworthstone/beads_viewer/pkg/loader"
	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	"github.com/Dicklesworthstone/beads_viewer/pkg/recipe"
)

func TestBackgroundWorker_NewWithoutPath(t *testing.T) {
	cfg := WorkerConfig{
		BeadsPath: "",
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if worker.State() != WorkerIdle {
		t.Errorf("Expected idle state, got %v", worker.State())
	}

	if worker.GetSnapshot() != nil {
		t.Error("Expected nil snapshot initially")
	}
}

func TestModelRepositoryCatalogMessagesReconcileSelectionAndIgnoreStale(t *testing.T) {
	m := Model{
		activeRepos:       map[string]bool{"ctx:a": true, "ctx:removed": true},
		catalogGeneration: 1,
	}
	updated, _ := m.Update(RepositoryCatalogReadyMsg{
		Generation: 2,
		Catalog: model.RepositoryCatalog{
			{ID: "ctx:a", Name: "a"},
			{ID: "ctx:new", Name: "new"},
		},
	})
	m = updated.(Model)
	if len(m.activeRepos) != 1 || !m.activeRepos["ctx:a"] || m.activeRepos["ctx:new"] {
		t.Fatalf("subset after addition/removal = %#v", m.activeRepos)
	}
	updated, _ = m.Update(RepositoryCatalogReadyMsg{
		Generation: 1,
		Catalog:    model.RepositoryCatalog{{ID: "ctx:stale", Name: "stale"}},
	})
	m = updated.(Model)
	if len(m.repositoryCatalog) != 2 || m.catalogGeneration != 2 {
		t.Fatalf("stale catalog was applied: generation=%d catalog=%#v", m.catalogGeneration, m.repositoryCatalog)
	}
	updated, _ = m.Update(RepositoryCatalogReadyMsg{
		Generation: 3,
		Catalog:    model.RepositoryCatalog{{ID: "ctx:new", Name: "new"}},
	})
	m = updated.(Model)
	if m.activeRepos != nil {
		t.Fatalf("emptied subset = %#v, want all", m.activeRepos)
	}
}

func TestOpenRepositoryPickerTracksLiveCatalogByExactID(t *testing.T) {
	m := NewModel(nil, nil, "")
	m.hubRepositoryMode = true
	m.repositoryCatalog = model.RepositoryCatalog{
		{ID: "ctx:a", Name: "alpha"},
		{ID: "ctx:b", Name: "beta"},
	}
	m.activeRepos = map[string]bool{"ctx:b": true}
	m.repoPicker = NewRepoPickerModel(m.repositoryCatalog, m.theme)
	m.repoPicker.SetActiveRepos(m.activeRepos)
	m.repoPicker.MoveDown()
	m.showRepoPicker = true

	updated, _ := m.Update(RepositoryCatalogReadyMsg{
		Generation: 1,
		Catalog: model.RepositoryCatalog{
			{ID: "ctx:b", Name: "aardvark", BeadCount: 4},
			{ID: "ctx:new", Name: "new", BeadCount: 1},
		},
	})
	m = updated.(Model)
	selected := m.repoPicker.SelectedRepos()
	if m.repoPicker.currentRepositoryID() != "ctx:b" {
		t.Fatalf("live refresh cursor = %q, want ctx:b", m.repoPicker.currentRepositoryID())
	}
	if len(selected) != 1 || !selected["ctx:b"] || selected["ctx:new"] {
		t.Fatalf("live refresh draft = %#v", selected)
	}
	if view := m.repoPicker.View(); !strings.Contains(view, "aardvark") || !strings.Contains(view, "(4)") {
		t.Fatalf("open picker did not refresh metadata:\n%s", view)
	}
}

func TestBackgroundWorkerMessageBufferHasSafeMinimum(t *testing.T) {
	t.Run("explicit", func(t *testing.T) {
		worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer worker.Stop()
		if got := cap(worker.msgCh); got != minWorkerMessageBuffer {
			t.Fatalf("message buffer = %d, want minimum %d", got, minWorkerMessageBuffer)
		}
	})
	t.Run("environment", func(t *testing.T) {
		t.Setenv("BV_CHANNEL_BUFFER", "1")
		worker, err := NewBackgroundWorker(WorkerConfig{})
		if err != nil {
			t.Fatal(err)
		}
		defer worker.Stop()
		if got := cap(worker.msgCh); got != minWorkerMessageBuffer {
			t.Fatalf("message buffer = %d, want minimum %d", got, minWorkerMessageBuffer)
		}
	})
}

func TestBackgroundWorkerBacklogPreservesSnapshot(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	worker.send(Phase2UpdateMsg{DataHash: "old-1"})
	worker.send(Phase2UpdateMsg{DataHash: "old-2"})
	snapshot := &DataSnapshot{DataHash: "snapshot"}
	worker.send(SnapshotReadyMsg{Snapshot: snapshot})
	worker.send(RepositoryCatalogReadyMsg{Generation: 1, Catalog: model.RepositoryCatalog{{ID: "ctx:a"}}})

	foundSnapshot := false
	for len(worker.msgCh) > 0 {
		if message, ok := (<-worker.msgCh).(SnapshotReadyMsg); ok && message.Snapshot == snapshot {
			foundSnapshot = true
		}
	}
	if !foundSnapshot {
		t.Fatal("high-priority snapshot was evicted from the bounded backlog")
	}
}

func TestBackgroundWorkerBacklogPreservesCurrentSnapshotAndNewestCatalog(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	oldSnapshot := &DataSnapshot{DataHash: "old", pooledIssues: []*model.Issue{{ID: "old"}}}
	currentSnapshot := &DataSnapshot{DataHash: "current"}
	worker.mu.Lock()
	worker.snapshot = currentSnapshot
	worker.mu.Unlock()
	worker.send(SnapshotReadyMsg{Snapshot: oldSnapshot, SnapshotVer: 1})
	worker.send(SnapshotReadyMsg{Snapshot: currentSnapshot, SnapshotVer: 2})
	worker.send(RepositoryCatalogReadyMsg{Generation: 3, Catalog: model.RepositoryCatalog{{ID: "ctx:new"}}})

	foundCurrent := false
	foundCatalog := false
	for len(worker.msgCh) > 0 {
		switch message := (<-worker.msgCh).(type) {
		case SnapshotReadyMsg:
			foundCurrent = message.Snapshot == currentSnapshot
			if message.CatalogAvailable && message.CatalogGeneration == 3 && len(message.Catalog) == 1 && message.Catalog[0].ID == "ctx:new" {
				foundCatalog = true
			}
		case RepositoryCatalogReadyMsg:
			foundCatalog = message.Generation == 3
		}
	}
	if !foundCurrent || !foundCatalog {
		t.Fatalf("bounded backlog lost state: current=%v catalog=%v", foundCurrent, foundCatalog)
	}
}

func TestBackgroundWorkerBacklogPreservesLatestSourceError(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	currentSnapshot := &DataSnapshot{DataHash: "current"}
	worker.mu.Lock()
	worker.snapshot = currentSnapshot
	worker.mu.Unlock()
	worker.send(SnapshotReadyMsg{Snapshot: currentSnapshot, SnapshotVer: 2})
	worker.send(RepositoryCatalogReadyMsg{Generation: 2, Catalog: model.RepositoryCatalog{{ID: "ctx:a"}}})
	worker.send(SnapshotErrorMsg{Err: errors.New("source unavailable"), Recoverable: true})

	foundSnapshot := false
	foundError := false
	for len(worker.msgCh) > 0 {
		switch message := (<-worker.msgCh).(type) {
		case SnapshotReadyMsg:
			foundSnapshot = message.Snapshot == currentSnapshot
		case SnapshotErrorMsg:
			foundError = message.Err != nil
		}
	}
	if !foundSnapshot || !foundError {
		t.Fatalf("bounded backlog lost current state or source error: snapshot=%v error=%v", foundSnapshot, foundError)
	}
}

func TestBackgroundWorkerBacklogDeliversCatalogThroughCurrentSnapshot(t *testing.T) {
	permutations := [][]string{
		{"catalog", "error", "snapshot"},
		{"catalog", "snapshot", "error"},
		{"error", "catalog", "snapshot"},
		{"error", "snapshot", "catalog"},
		{"snapshot", "catalog", "error"},
		{"snapshot", "error", "catalog"},
	}
	for _, order := range permutations {
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 2})
			if err != nil {
				t.Fatal(err)
			}
			defer worker.Stop()
			catalog := model.RepositoryCatalog{{ID: "ctx:current", Name: "current"}}
			current := NewSnapshotBuilder([]model.Issue{{ID: "CURRENT", Title: "Current", Status: model.StatusOpen, IssueType: model.TypeTask}}).Build()
			worker.mu.Lock()
			worker.snapshot = current
			worker.catalog = catalog
			worker.mu.Unlock()
			messages := map[string]tea.Msg{
				"catalog": RepositoryCatalogReadyMsg{Catalog: catalog, Generation: 6},
				"error":   SnapshotErrorMsg{Err: errors.New("source unavailable"), Recoverable: true},
				"snapshot": SnapshotReadyMsg{
					Snapshot:          current,
					SnapshotVer:       5,
					Catalog:           model.RepositoryCatalog{{ID: "ctx:snapshot-old", Name: "snapshot-old"}},
					CatalogGeneration: 5,
					CatalogAvailable:  true,
					CatalogChanged:    false,
				},
			}
			for _, name := range order {
				worker.send(messages[name])
			}

			retained := make([]tea.Msg, 0, 2)
			for len(worker.msgCh) > 0 {
				retained = append(retained, <-worker.msgCh)
			}
			if len(retained) != 2 {
				t.Fatalf("retained %d messages, want 2", len(retained))
			}
			if _, ok := retained[0].(RepositoryCatalogReadyMsg); ok {
				t.Fatal("redundant standalone catalog remained in bounded backlog")
			}
			if _, ok := retained[1].(RepositoryCatalogReadyMsg); ok {
				t.Fatal("redundant standalone catalog remained in bounded backlog")
			}
			expectedOrder := make([]string, 0, 2)
			for _, name := range order {
				if name != "catalog" {
					expectedOrder = append(expectedOrder, name)
				}
			}
			for i, message := range retained {
				got := ""
				switch message.(type) {
				case SnapshotErrorMsg:
					got = "error"
				case SnapshotReadyMsg:
					got = "snapshot"
				}
				if got != expectedOrder[i] {
					t.Fatalf("retained order[%d] = %q, want %q", i, got, expectedOrder[i])
				}
			}

			m := NewModel(nil, nil, "")
			m.catalogGeneration = 4
			m.repositoryCatalog = model.RepositoryCatalog{{ID: "ctx:old", Name: "old"}}
			for _, message := range retained {
				updated, _ := m.Update(message)
				m = updated.(Model)
			}
			if len(m.repositoryCatalog) != 1 || m.repositoryCatalog[0].ID != "ctx:current" {
				t.Fatalf("model catalog = %#v, want current snapshot catalog", m.repositoryCatalog)
			}
			if len(m.issues) != 1 || m.issues[0].ID != "CURRENT" {
				t.Fatalf("model issues = %#v, want current snapshot", m.issues)
			}
		})
	}
}

func TestBackgroundWorkerBacklogDoesNotDowngradeNewerSnapshotCatalog(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	newerCatalog := model.RepositoryCatalog{{ID: "ctx:newer", Name: "newer"}}
	current := NewSnapshotBuilder([]model.Issue{{ID: "CURRENT", Title: "Current", Status: model.StatusOpen, IssueType: model.TypeTask}}).Build()
	worker.mu.Lock()
	worker.snapshot = current
	worker.catalog = newerCatalog
	worker.mu.Unlock()
	worker.send(RepositoryCatalogReadyMsg{Catalog: model.RepositoryCatalog{{ID: "ctx:older", Name: "older"}}, Generation: 5})
	worker.send(SnapshotErrorMsg{Err: errors.New("source unavailable"), Recoverable: true})
	worker.send(SnapshotReadyMsg{
		Snapshot:          current,
		SnapshotVer:       6,
		Catalog:           newerCatalog,
		CatalogGeneration: 6,
		CatalogAvailable:  true,
	})

	retained := drainWorkerMessages(worker)
	if len(retained) != 2 {
		t.Fatalf("retained %d messages, want 2", len(retained))
	}
	if _, ok := retained[0].(SnapshotErrorMsg); !ok {
		t.Fatalf("first retained message = %T, want source error", retained[0])
	}
	snapshot, ok := retained[1].(SnapshotReadyMsg)
	if !ok {
		t.Fatalf("second retained message = %T, want snapshot", retained[1])
	}
	if snapshot.CatalogGeneration != 6 || len(snapshot.Catalog) != 1 || snapshot.Catalog[0].ID != "ctx:newer" {
		t.Fatalf("snapshot catalog was downgraded: generation=%d catalog=%#v", snapshot.CatalogGeneration, snapshot.Catalog)
	}

	m := NewModel(nil, nil, "")
	for _, message := range retained {
		updated, _ := m.Update(message)
		m = updated.(Model)
	}
	if len(m.repositoryCatalog) != 1 || m.repositoryCatalog[0].ID != "ctx:newer" || m.catalogGeneration != 6 {
		t.Fatalf("model catalog = %#v generation=%d", m.repositoryCatalog, m.catalogGeneration)
	}
}

func TestBackgroundWorkerBacklogDoesNotClearNewerSnapshotCatalogError(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	current := &DataSnapshot{DataHash: "current"}
	newerError := errors.New("newer catalog unavailable")
	worker.mu.Lock()
	worker.snapshot = current
	worker.mu.Unlock()
	worker.send(RepositoryCatalogReadyMsg{Catalog: model.RepositoryCatalog{{ID: "ctx:older"}}, Generation: 5})
	worker.send(SnapshotErrorMsg{Err: errors.New("source unavailable"), Recoverable: true})
	worker.send(SnapshotReadyMsg{
		Snapshot:          current,
		SnapshotVer:       6,
		CatalogGeneration: 6,
		CatalogAvailable:  false,
		CatalogError:      newerError,
	})

	retained := drainWorkerMessages(worker)
	snapshot, ok := retained[1].(SnapshotReadyMsg)
	if !ok {
		t.Fatalf("second retained message = %T, want snapshot", retained[1])
	}
	if snapshot.CatalogGeneration != 6 || snapshot.CatalogAvailable || !errors.Is(snapshot.CatalogError, newerError) {
		t.Fatalf("newer unavailable state was downgraded: generation=%d available=%v error=%v", snapshot.CatalogGeneration, snapshot.CatalogAvailable, snapshot.CatalogError)
	}
}

func TestBackgroundWorkerBacklogEqualGenerationMergesDeliveryState(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	snapshotCatalog := model.RepositoryCatalog{{ID: "ctx:snapshot", Name: "snapshot"}}
	current := &DataSnapshot{DataHash: "current"}
	worker.mu.Lock()
	worker.snapshot = current
	worker.mu.Unlock()
	worker.send(RepositoryCatalogReadyMsg{
		Catalog:    model.RepositoryCatalog{{ID: "ctx:standalone", Name: "standalone"}},
		Generation: 6,
		Recovered:  true,
	})
	worker.send(SnapshotErrorMsg{Err: errors.New("source unavailable"), Recoverable: true})
	worker.send(SnapshotReadyMsg{
		Snapshot:          current,
		SnapshotVer:       6,
		Catalog:           snapshotCatalog,
		CatalogGeneration: 6,
		CatalogAvailable:  true,
		CatalogChanged:    false,
		CatalogRecovered:  false,
		CatalogError:      errors.New("stale catalog error"),
	})

	retained := drainWorkerMessages(worker)
	snapshot, ok := retained[1].(SnapshotReadyMsg)
	if !ok {
		t.Fatalf("second retained message = %T, want snapshot", retained[1])
	}
	if len(snapshot.Catalog) != 1 || snapshot.Catalog[0].ID != "ctx:snapshot" {
		t.Fatalf("equal generation replaced snapshot payload: %#v", snapshot.Catalog)
	}
	if !snapshot.CatalogChanged || !snapshot.CatalogRecovered || snapshot.CatalogError != nil {
		t.Fatalf("equal generation state not merged: changed=%v recovered=%v error=%v", snapshot.CatalogChanged, snapshot.CatalogRecovered, snapshot.CatalogError)
	}
}

func TestBackgroundWorkerBacklogEqualGenerationCompletesUnavailableSnapshot(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	catalog := model.RepositoryCatalog{{ID: "ctx:recovered"}}
	current := &DataSnapshot{DataHash: "current"}
	worker.mu.Lock()
	worker.snapshot = current
	worker.mu.Unlock()
	worker.send(RepositoryCatalogReadyMsg{Catalog: catalog, Generation: 6, Recovered: true})
	worker.send(SnapshotErrorMsg{Err: errors.New("source unavailable"), Recoverable: true})
	worker.send(SnapshotReadyMsg{
		Snapshot:          current,
		SnapshotVer:       6,
		CatalogGeneration: 6,
		CatalogAvailable:  false,
		CatalogError:      errors.New("transient catalog error"),
	})

	retained := drainWorkerMessages(worker)
	snapshot, ok := retained[1].(SnapshotReadyMsg)
	if !ok {
		t.Fatalf("second retained message = %T, want snapshot", retained[1])
	}
	if !snapshot.CatalogAvailable || !snapshot.CatalogChanged || !snapshot.CatalogRecovered || snapshot.CatalogError != nil ||
		len(snapshot.Catalog) != 1 || snapshot.Catalog[0].ID != "ctx:recovered" {
		t.Fatalf("equal recovery did not complete snapshot: %#v", snapshot)
	}
}

func drainWorkerMessages(worker *BackgroundWorker) []tea.Msg {
	messages := make([]tea.Msg, 0, len(worker.msgCh))
	for len(worker.msgCh) > 0 {
		messages = append(messages, <-worker.msgCh)
	}
	return messages
}

func TestBackgroundWorkerStopReleasesQueuedSnapshots(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{MessageBuffer: 2})
	if err != nil {
		t.Fatal(err)
	}
	queued := &DataSnapshot{pooledIssues: []*model.Issue{{ID: "queued"}}}
	worker.send(SnapshotReadyMsg{Snapshot: queued, SnapshotVer: 1})
	worker.Stop()
	if queued.pooledIssues != nil {
		t.Fatal("shutdown retained pooled references from a queued snapshot")
	}
}

func TestModelIgnoresOutOfOrderBackgroundSnapshot(t *testing.T) {
	newer := NewSnapshotBuilder([]model.Issue{{ID: "NEW", Title: "New", Status: model.StatusOpen, IssueType: model.TypeTask}}).Build()
	older := NewSnapshotBuilder([]model.Issue{{ID: "OLD", Title: "Old", Status: model.StatusOpen, IssueType: model.TypeTask}}).Build()
	m := NewModel(nil, nil, "")
	updated, _ := m.Update(SnapshotReadyMsg{
		Snapshot:          newer,
		SnapshotVer:       2,
		Catalog:           model.RepositoryCatalog{{ID: "ctx:new"}},
		CatalogGeneration: 2,
		CatalogChanged:    true,
	})
	m = updated.(Model)
	updated, _ = m.Update(SnapshotReadyMsg{
		Snapshot:          older,
		SnapshotVer:       1,
		Catalog:           model.RepositoryCatalog{{ID: "ctx:old"}},
		CatalogGeneration: 1,
		CatalogChanged:    true,
	})
	m = updated.(Model)
	if len(m.issues) != 1 || m.issues[0].ID != "NEW" || m.lastSnapshotVersion != 2 {
		t.Fatalf("out-of-order snapshot applied: issues=%#v version=%d", m.issues, m.lastSnapshotVersion)
	}
	if len(m.repositoryCatalog) != 1 || m.repositoryCatalog[0].ID != "ctx:new" {
		t.Fatalf("out-of-order catalog applied: %#v", m.repositoryCatalog)
	}
}

func TestModelHubCatalogRespectsAutoRefreshOptOut(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/a"})
	t.Setenv("BV_BACKGROUND_MODE", "1")
	t.Setenv("BV_HUB_AUTO_REFRESH", "0")
	t.Setenv("BV_HUB_CHANGE_SIGNAL", filepath.Join(directory, "viewer-generation"))
	m := NewModel(nil, nil, issuesPath)
	defer m.Stop()
	m.SetHistoryProvider("external", configPath)
	if m.backgroundWorker == nil || m.backgroundWorker.hubConfigPath != configPath {
		t.Fatal("manual catalog refresh was not configured")
	}
	if m.backgroundWorker.hubConfigWatcher != nil || m.backgroundWorker.hubChangeWatcher != nil {
		t.Fatal("Hub auto-refresh opt-out left a Hub watcher enabled")
	}
}

func TestModelDirectHubModeEnablesConfigWatcher(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/a"})
	t.Setenv("BV_BACKGROUND_MODE", "")
	t.Setenv("BV_HUB_CHANGE_SIGNAL", "")
	t.Setenv("BV_HUB_AUTO_REFRESH", "1")
	m := NewModel(nil, nil, issuesPath)
	defer m.Stop()
	if m.backgroundWorker != nil || m.watcher == nil {
		t.Fatal("direct mode did not start with the ordinary file watcher")
	}
	m.SetHistoryProvider("external", configPath)
	if m.backgroundWorker == nil || m.backgroundWorker.hubConfigWatcher == nil || m.watcher == nil {
		t.Fatal("Hub provider did not retain the file watcher during worker transition")
	}
	if err := m.backgroundWorker.Start(); err != nil {
		t.Fatal(err)
	}
	m.backgroundWorker.TriggerRefresh()
	initial := waitForSnapshotReady(t, m.backgroundWorker.Messages())
	updated, _ := m.Update(initial)
	m = updated.(Model)
	if m.watcher != nil {
		t.Fatal("fallback file watcher remained after the Hub worker produced a snapshot")
	}
	temporary := filepath.Join(directory, "hub.yaml.next")
	writeWorkerHubConfig(t, temporary, map[string]string{"ctx:b": "/b"})
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
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/a"})
	t.Setenv("BV_BACKGROUND_MODE", "")
	t.Setenv("BV_HUB_CHANGE_SIGNAL", "")
	t.Setenv("BV_HUB_AUTO_REFRESH", "1")
	m := NewModel(nil, nil, issuesPath)
	defer m.Stop()
	m.SetHistoryProvider("external", configPath)
	if m.backgroundWorker == nil || m.watcher == nil || !m.watcher.IsStarted() {
		t.Fatal("Hub transition did not retain a live fallback watcher")
	}
	updated, cmd := m.Update(SnapshotErrorMsg{Err: errors.New("start failed"), StartFailure: true})
	m = updated.(Model)
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
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:empty": "/empty"})
	t.Setenv("BV_BACKGROUND_MODE", "")
	t.Setenv("BV_HUB_CHANGE_SIGNAL", "")
	t.Setenv("BV_HUB_AUTO_REFRESH", "1")
	m := NewModel(nil, nil, issuesPath)
	defer m.Stop()
	m.SetRepositoryCatalogIssues(nil)
	m.SetHistoryProvider("external", configPath)
	if len(m.repositoryCatalog) != 1 || m.repositoryCatalog[0].ID != "ctx:empty" || m.repositoryCatalog[0].BeadCount != 0 {
		t.Fatalf("empty Hub catalog = %#v", m.repositoryCatalog)
	}
	if m.backgroundWorker == nil || !m.snapshotInitPending {
		t.Fatal("empty Hub did not remain active for background startup")
	}
}

func TestModelHubCatalogCountsUnfilteredStartupIssues(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "hub.yaml")
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/a"})
	filtered := []model.Issue{{ID: "OPEN", Labels: []string{"ctx:a"}}}
	all := []model.Issue{
		{ID: "OPEN", Labels: []string{"ctx:a"}},
		{ID: "CLOSED", Labels: []string{"ctx:a"}},
	}
	m := NewModel(filtered, nil, "")
	m.SetRepositoryCatalogIssues(all)
	m.SetHistoryProvider("external", configPath)
	if got := catalogEntry(m.repositoryCatalog, "ctx:a").BeadCount; got != 2 {
		t.Fatalf("unfiltered startup count = %d, want 2", got)
	}
}

func TestModelWorkspaceCatalogIgnoresHubCatalogMessages(t *testing.T) {
	m := Model{
		workspaceMode: true,
		repositoryCatalog: model.RepositoryCatalog{
			{ID: "api", Name: "api", Kind: model.RepositoryIdentityWorkspacePrefix},
		},
	}
	updated, _ := m.Update(RepositoryCatalogReadyMsg{
		Generation: 3,
		Catalog: model.RepositoryCatalog{
			{ID: "ctx:api", Name: "api", Kind: model.RepositoryIdentityHubContext},
		},
	})
	m = updated.(Model)
	if len(m.repositoryCatalog) != 1 || m.repositoryCatalog[0].ID != "api" || m.catalogGeneration != 0 {
		t.Fatalf("Hub catalog replaced workspace catalog: %#v", m.repositoryCatalog)
	}
	updated, _ = m.Update(RepositoryCatalogErrorMsg{Generation: 4, Err: errors.New("Hub config unavailable")})
	m = updated.(Model)
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
	m = updated.(Model)
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
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/team/a/repo", "ctx:zero": "/team/zero"})
	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:       issuesPath,
		HubConfigPath:   configPath,
		SourceRetryBase: time.Hour,
	})
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

	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/renamed/a/repo", "ctx:new": "/team/new"})
	worker.markCatalogDirty()
	worker.process()
	changed := waitForCatalogReady(t, worker.Messages())
	if worker.LastHash() != initialHash {
		t.Fatal("catalog-only refresh changed issue snapshot hash")
	}
	if len(changed.Catalog) != 2 || catalogEntry(changed.Catalog, "ctx:a").Path != "/renamed/a/repo" || catalogEntry(changed.Catalog, "ctx:new").BeadCount != 0 {
		t.Fatalf("changed catalog = %#v", changed.Catalog)
	}
	if err := os.WriteFile(issuesPath, []byte(
		`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task","labels":["ctx:a"]}`+"\n"+
			`{"id":"TWO","title":"Two","status":"open","priority":1,"issue_type":"task","labels":["ctx:new"]}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	worker.process()
	counted := waitForCatalogReady(t, worker.Messages())
	if catalogEntry(counted.Catalog, "ctx:new").BeadCount != 1 {
		t.Fatalf("issue-count refresh catalog = %#v", counted.Catalog)
	}

	if err := os.WriteFile(configPath, []byte("version: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	worker.markCatalogDirty()
	worker.process()
	waitForCatalogError(t, worker.Messages())
	if got := catalogEntry(worker.catalog, "ctx:a").Path; got != "/renamed/a/repo" {
		t.Fatalf("transient failure replaced last valid catalog: %q", got)
	}

	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:recovered": "/team/recovered"})
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
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repositories := map[string]string{"ctx:a": "/a"}
	writeWorkerHubConfig(t, configPath, repositories)
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, HubConfigPath: configPath, SourceRetryBase: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	worker.process()
	initial := waitForCatalogReady(t, worker.Messages())

	if err := os.WriteFile(configPath, []byte("version: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	worker.markCatalogDirty()
	worker.process()
	loadError := waitForCatalogError(t, worker.Messages())
	m := Model{}
	updated, _ := m.Update(initial)
	m = updated.(Model)
	updated, _ = m.Update(loadError)
	m = updated.(Model)
	if !m.statusIsError {
		t.Fatal("catalog failure did not set model error state")
	}

	writeWorkerHubConfig(t, configPath, repositories)
	worker.markCatalogDirty()
	worker.process()
	recovered := waitForCatalogReady(t, worker.Messages())
	if !recovered.Recovered {
		t.Fatal("identical catalog recovery was not emitted explicitly")
	}
	updated, _ = m.Update(recovered)
	m = updated.(Model)
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
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/a"})
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, HubConfigPath: configPath, MessageBuffer: 1})
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
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/a"})
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, HubConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	worker.catalogLoader = func(_ string, issues []model.Issue) (model.RepositoryCatalog, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return model.RepositoryCatalog{{ID: "ctx:stale", Name: "stale"}}, nil
		}
		return model.RepositoryCatalog{{ID: "ctx:fresh", Name: "fresh"}}, nil
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
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, HubConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	loadedCount := 0
	worker.catalogLoader = func(_ string, issues []model.Issue) (model.RepositoryCatalog, error) {
		loadedCount = len(issues)
		return nil, nil
	}
	_, err = worker.buildRepositoryCatalog(&DataSnapshot{
		Issues:         []model.Issue{{ID: "OPEN"}},
		LoadedOpenOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if loadedCount != 2 {
		t.Fatalf("catalog issue count = %d, want complete set of 2", loadedCount)
	}
}

func TestBackgroundWorkerWatchesAtomicHubConfigReplacement(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	configPath := filepath.Join(directory, "hub.yaml")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeWorkerHubConfig(t, configPath, map[string]string{"ctx:a": "/a"})
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath, HubConfigPath: configPath, DebounceDelay: 5 * time.Millisecond})
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
	writeWorkerHubConfig(t, temporary, map[string]string{"ctx:b": "/b"})
	if err := os.Rename(temporary, configPath); err != nil {
		t.Fatal(err)
	}
	ready := waitForCatalogReady(t, worker.Messages())
	if len(ready.Catalog) != 1 || ready.Catalog[0].ID != "ctx:b" {
		t.Fatalf("atomic replacement catalog = %#v", ready.Catalog)
	}
}

func writeWorkerHubConfig(t *testing.T, path string, repositories map[string]string) {
	t.Helper()
	var builder strings.Builder
	builder.WriteString("version: 1\nstore: store\nledger: ledger\nrepositories:\n")
	keys := make([]string, 0, len(repositories))
	for key := range repositories {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&builder, "  %s:\n    path: %s\n", key, repositories[key])
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForCatalogReady(t *testing.T, messages <-chan tea.Msg) RepositoryCatalogReadyMsg {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case message := <-messages:
			if ready, ok := message.(RepositoryCatalogReadyMsg); ok {
				return ready
			}
			if ready, ok := message.(SnapshotReadyMsg); ok && (ready.CatalogChanged || ready.CatalogRecovered) {
				return RepositoryCatalogReadyMsg{Catalog: ready.Catalog, Generation: ready.CatalogGeneration, Recovered: ready.CatalogRecovered}
			}
		case <-timer.C:
			t.Fatal("timed out waiting for repository catalog")
		}
	}
}

func waitForCatalogError(t *testing.T, messages <-chan tea.Msg) RepositoryCatalogErrorMsg {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case message := <-messages:
			if loadError, ok := message.(RepositoryCatalogErrorMsg); ok {
				return loadError
			}
			if update, ok := message.(SnapshotReadyMsg); ok && update.CatalogError != nil {
				return RepositoryCatalogErrorMsg{Err: update.CatalogError, Generation: update.CatalogGeneration}
			}
		case <-timer.C:
			t.Fatal("timed out waiting for repository catalog error")
		}
	}
}

func waitForSnapshotReady(t *testing.T, messages <-chan tea.Msg) SnapshotReadyMsg {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case message := <-messages:
			if ready, ok := message.(SnapshotReadyMsg); ok {
				return ready
			}
		case <-timer.C:
			t.Fatal("timed out waiting for snapshot")
		}
	}
}

func catalogEntry(catalog model.RepositoryCatalog, id string) model.RepositoryCatalogEntry {
	for _, entry := range catalog {
		if entry.ID == id {
			return entry
		}
	}
	return model.RepositoryCatalogEntry{}
}

func TestBackgroundWorker_NewWithoutPath_EnvDefaults(t *testing.T) {
	t.Setenv("BV_DEBOUNCE_MS", "123")
	t.Setenv("BV_CHANNEL_BUFFER", "3")
	t.Setenv("BV_HEARTBEAT_INTERVAL_S", "9")
	t.Setenv("BV_WATCHDOG_INTERVAL_S", "11")

	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: ""})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if worker.debounceDelay != 123*time.Millisecond {
		t.Errorf("debounceDelay=%v, want %v", worker.debounceDelay, 123*time.Millisecond)
	}
	if cap(worker.msgCh) != 3 {
		t.Errorf("cap(msgCh)=%d, want %d", cap(worker.msgCh), 3)
	}
	if worker.heartbeatInterval != 9*time.Second {
		t.Errorf("heartbeatInterval=%v, want %v", worker.heartbeatInterval, 9*time.Second)
	}
	if worker.watchdogInterval != 11*time.Second {
		t.Errorf("watchdogInterval=%v, want %v", worker.watchdogInterval, 11*time.Second)
	}
}

func TestEnvMaxLineSizeBytes(t *testing.T) {
	t.Setenv("BV_MAX_LINE_SIZE_MB", "12")
	if got := envMaxLineSizeBytes(); got != 12*1024*1024 {
		t.Errorf("envMaxLineSizeBytes()=%d, want %d", got, 12*1024*1024)
	}

	t.Setenv("BV_MAX_LINE_SIZE_MB", "-1")
	if got := envMaxLineSizeBytes(); got != 0 {
		t.Errorf("envMaxLineSizeBytes() with invalid env=%d, want %d", got, 0)
	}
}

func TestBackgroundWorker_NewWithPath(t *testing.T) {
	// Create a temporary beads file
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	// Write a valid beads file
	content := `{"id":"test-1","title":"Test Issue","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if worker.State() != WorkerIdle {
		t.Errorf("Expected idle state, got %v", worker.State())
	}
}

func TestBackgroundWorker_StartStop(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should be idempotent
	worker.Stop()
	worker.Stop() // Should not panic

	if worker.State() != WorkerStopped {
		t.Errorf("Expected stopped state, got %v", worker.State())
	}
}

func TestBackgroundWorker_StopReturnsSnapshotPooledIssues(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")
	if err := os.WriteFile(beadsPath, []byte(`{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: beadsPath})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}

	pooled := loader.GetIssue()
	pooled.ID = "pooled-1"
	pooled.Labels = append(pooled.Labels, "backend")
	worker.snapshot = &DataSnapshot{
		Issues:           []model.Issue{{ID: "test-1", Title: "Test", Status: model.StatusOpen}},
		pooledIssues:     []*model.Issue{pooled},
		CreatedAt:        time.Now(),
		phase2Ready:      true,
		LoadWarningCount: 0,
	}

	worker.Stop()

	if pooled.ID != "" {
		t.Fatalf("expected pooled issue to be reset on Stop, got ID %q", pooled.ID)
	}
	if len(pooled.Labels) != 0 {
		t.Fatalf("expected pooled issue labels to be cleared on Stop, got %v", pooled.Labels)
	}
	if worker.snapshot != nil {
		t.Fatal("expected worker snapshot to be cleared on Stop")
	}
}

func TestModelStopReturnsSnapshotPooledIssuesWithoutWorker(t *testing.T) {
	pooled := loader.GetIssue()
	pooled.ID = "pooled-model"
	pooled.Comments = append(pooled.Comments, &model.Comment{ID: "1", Text: "hello"})

	m := Model{
		snapshot: &DataSnapshot{
			Issues:       []model.Issue{{ID: "A", Title: "Issue A", Status: model.StatusOpen}},
			pooledIssues: []*model.Issue{pooled},
		},
	}

	m.Stop()

	if pooled.ID != "" {
		t.Fatalf("expected pooled issue to be reset on Model.Stop, got ID %q", pooled.ID)
	}
	if len(pooled.Comments) != 0 {
		t.Fatalf("expected pooled issue comments to be cleared on Model.Stop, got %d", len(pooled.Comments))
	}
	if m.snapshot == nil || len(m.snapshot.pooledIssues) != 0 {
		t.Fatal("expected snapshot pooled refs to be cleared on Model.Stop")
	}
}

func TestBackgroundWorker_TriggerRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// Trigger refresh and wait for processing
	worker.TriggerRefresh()

	// Wait for processing to complete
	time.Sleep(200 * time.Millisecond)

	snapshot := worker.GetSnapshot()
	if snapshot == nil {
		t.Fatal("Expected snapshot after refresh")
	}

	if len(snapshot.Issues) != 1 {
		t.Errorf("Expected 1 issue, got %d", len(snapshot.Issues))
	}
}

func TestBackgroundWorker_WatcherChanged(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	ch := worker.WatcherChanged()
	if ch == nil {
		t.Error("WatcherChanged should return non-nil channel")
	}
}

func TestBackgroundWorker_WatcherChangedNil(t *testing.T) {
	// Worker without path should have nil watcher
	cfg := WorkerConfig{
		BeadsPath: "",
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if worker.WatcherChanged() != nil {
		t.Error("WatcherChanged should return nil when no watcher")
	}
}

func TestWorkerState_String(t *testing.T) {
	tests := []struct {
		state    WorkerState
		expected string
	}{
		{WorkerIdle, "0"},
		{WorkerProcessing, "1"},
		{WorkerStopped, "2"},
	}

	for _, tt := range tests {
		// Just verify the states have distinct values
		if int(tt.state) < 0 || int(tt.state) > 2 {
			t.Errorf("Unexpected state value: %v", tt.state)
		}
	}
}

func TestBackgroundWorker_ContentHashDedup(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test Issue","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// First refresh should build snapshot and set hash
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)
	waitForWorkerIdle(t, worker, 1)

	snapshot1 := worker.GetSnapshot()
	if snapshot1 == nil {
		t.Fatal("Expected snapshot after first refresh")
	}

	hash1 := worker.LastHash()
	if hash1 == "" {
		t.Error("Expected non-empty hash after first refresh")
	}

	// Second refresh with same content should be deduped (snapshot unchanged)
	worker.TriggerRefresh()
	waitForWorkerIdle(t, worker, 2)

	snapshot2 := worker.GetSnapshot()
	hash2 := worker.LastHash()

	// Hash should be the same
	if hash1 != hash2 {
		t.Errorf("Hash changed unexpectedly: %s -> %s", hash1, hash2)
	}

	// Snapshot pointer should be unchanged (deduped)
	if snapshot1 != snapshot2 {
		t.Error("Snapshot pointer changed when content was unchanged - dedup failed")
	}
}

func TestBackgroundWorker_ContentHashChanges(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content1 := `{"id":"test-1","title":"Test Issue","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content1), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// First refresh
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)
	waitForWorkerIdle(t, worker, 1)

	snapshot1 := worker.GetSnapshot()
	if snapshot1 == nil {
		t.Fatal("Expected snapshot after first refresh")
	}
	hash1 := worker.LastHash()

	// Modify the file content
	content2 := `{"id":"test-1","title":"Updated Title","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content2), 0644); err != nil {
		t.Fatalf("Failed to write modified file: %v", err)
	}

	// Second refresh with different content should rebuild
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 2)
	waitForWorkerIdle(t, worker, 2)

	snapshot2 := worker.GetSnapshot()
	if snapshot2 == nil {
		t.Fatal("Expected snapshot after second refresh")
	}
	hash2 := worker.LastHash()

	// Hash should be different
	if hash1 == hash2 {
		t.Error("Hash should have changed when content changed")
	}

	// Snapshot should be different
	if snapshot1 == snapshot2 {
		t.Error("Snapshot pointer should have changed when content changed")
	}

	// New snapshot should have updated title
	if snapshot2.Issues[0].Title != "Updated Title" {
		t.Errorf("Expected updated title, got %q", snapshot2.Issues[0].Title)
	}
}

func TestBackgroundWorker_MetricsSnapshot(t *testing.T) {
	t.Setenv("BV_WORKER_METRICS", "1")

	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")
	content := strings.Join([]string{
		`{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}`,
		`{"id":"test-2","title":"Test 2","status":"open","priority":2,"issue_type":"feature"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	worker.TriggerRefresh()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if worker.GetSnapshot() != nil && worker.Metrics().SnapshotVersion > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if worker.GetSnapshot() == nil {
		t.Fatal("Expected snapshot after refresh")
	}

	metrics := worker.Metrics()
	if metrics.ProcessingCount == 0 {
		t.Fatalf("expected ProcessingCount > 0, got %d", metrics.ProcessingCount)
	}
	if metrics.SnapshotVersion == 0 {
		t.Fatalf("expected SnapshotVersion > 0")
	}
	if metrics.LastSnapshotReadyAt.IsZero() {
		t.Fatal("expected LastSnapshotReadyAt to be set")
	}
	if metrics.SnapshotSizeBytes <= 0 {
		t.Fatalf("expected SnapshotSizeBytes > 0, got %d", metrics.SnapshotSizeBytes)
	}
}

func TestBackgroundWorker_IncrementalListMetrics(t *testing.T) {
	t.Setenv("BV_WORKER_METRICS", "1")

	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	var builder strings.Builder
	for i := 0; i < 10; i++ {
		builder.WriteString(fmt.Sprintf(
			`{"id":"issue-%d","title":"Issue %d","status":"open","priority":%d,"issue_type":"task"}`+"\n",
			i, i, i,
		))
	}
	if err := os.WriteFile(beadsPath, []byte(builder.String()), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)

	if snap := worker.GetSnapshot(); snap == nil {
		t.Fatal("Expected snapshot after first refresh")
	} else if snap.IncrementalListUsed {
		t.Fatalf("expected first snapshot to be full rebuild")
	}

	updated := builder.String()
	updated = strings.Replace(updated, `"title":"Issue 0"`, `"title":"Issue 0 updated"`, 1)
	if err := os.WriteFile(beadsPath, []byte(updated), 0644); err != nil {
		t.Fatalf("Failed to write modified file: %v", err)
	}

	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 2)

	snap2 := worker.GetSnapshot()
	if snap2 == nil {
		t.Fatal("Expected snapshot after second refresh")
	}
	if !snap2.IncrementalListUsed {
		t.Fatalf("expected incremental list path on second snapshot")
	}

	metrics := worker.Metrics()
	if metrics.IncrementalListCount == 0 {
		t.Fatalf("expected IncrementalListCount > 0, got %d", metrics.IncrementalListCount)
	}
	if metrics.FullListCount == 0 {
		t.Fatalf("expected FullListCount > 0, got %d", metrics.FullListCount)
	}
	if metrics.IncrementalListRatio <= 0 {
		t.Fatalf("expected IncrementalListRatio > 0, got %f", metrics.IncrementalListRatio)
	}
}

func TestBackgroundWorker_LargeDatasetWarning(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	const issueCount = 5000
	f, err := os.Create(beadsPath)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	writer := bufio.NewWriter(f)
	for i := 0; i < issueCount; i++ {
		line := fmt.Sprintf(`{"id":"issue-%d","title":"Issue %d","status":"open","priority":1,"issue_type":"task"}`+"\n", i, i)
		if _, err := writer.WriteString(line); err != nil {
			_ = f.Close()
			t.Fatalf("Failed to write test file: %v", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = f.Close()
		t.Fatalf("Failed to flush test file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Failed to close test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	worker.TriggerRefresh()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if worker.GetSnapshot() != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	snapshot := worker.GetSnapshot()
	if snapshot == nil {
		t.Fatal("Expected snapshot after refresh")
	}
	if snapshot.DatasetTier != datasetTierLarge {
		t.Fatalf("expected datasetTierLarge, got %v", snapshot.DatasetTier)
	}
	if snapshot.SourceIssueCountHint != issueCount {
		t.Fatalf("expected SourceIssueCountHint=%d, got %d", issueCount, snapshot.SourceIssueCountHint)
	}
	if snapshot.LoadedOpenOnly {
		t.Fatalf("expected LoadedOpenOnly=false for large tier")
	}
	if snapshot.TruncatedCount != 0 {
		t.Fatalf("expected TruncatedCount=0, got %d", snapshot.TruncatedCount)
	}
	if snapshot.LargeDatasetWarning == "" {
		t.Fatal("expected LargeDatasetWarning to be populated")
	}
}

func TestBackgroundWorker_HugeDatasetOpenOnly(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	const issueCount = 20000
	f, err := os.Create(beadsPath)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	writer := bufio.NewWriter(f)
	openCount := 0
	for i := 0; i < issueCount; i++ {
		status := "open"
		if i%2 == 0 {
			status = "closed"
		} else {
			openCount++
		}
		line := fmt.Sprintf(`{"id":"issue-%d","title":"Issue %d","status":"%s","priority":1,"issue_type":"task"}`+"\n", i, i, status)
		if _, err := writer.WriteString(line); err != nil {
			_ = f.Close()
			t.Fatalf("Failed to write test file: %v", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = f.Close()
		t.Fatalf("Failed to flush test file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Failed to close test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	worker.TriggerRefresh()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if worker.GetSnapshot() != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	snapshot := worker.GetSnapshot()
	if snapshot == nil {
		t.Fatal("Expected snapshot after refresh")
	}
	if snapshot.DatasetTier != datasetTierHuge {
		t.Fatalf("expected datasetTierHuge, got %v", snapshot.DatasetTier)
	}
	if snapshot.SourceIssueCountHint != issueCount {
		t.Fatalf("expected SourceIssueCountHint=%d, got %d", issueCount, snapshot.SourceIssueCountHint)
	}
	if !snapshot.LoadedOpenOnly {
		t.Fatalf("expected LoadedOpenOnly=true for huge tier")
	}
	if len(snapshot.Issues) != openCount {
		t.Fatalf("expected %d open issues, got %d", openCount, len(snapshot.Issues))
	}
	expectedTruncated := issueCount - openCount
	if snapshot.TruncatedCount != expectedTruncated {
		t.Fatalf("expected TruncatedCount=%d, got %d", expectedTruncated, snapshot.TruncatedCount)
	}
	if !strings.Contains(snapshot.LargeDatasetWarning, "open-only") {
		t.Fatalf("expected LargeDatasetWarning to mention open-only, got %q", snapshot.LargeDatasetWarning)
	}
}

func TestBackgroundWorker_ResetHash(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// First refresh
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	snapshot1 := worker.GetSnapshot()
	hash1 := worker.LastHash()
	if hash1 == "" {
		t.Error("Expected non-empty hash")
	}

	// Reset hash
	worker.ResetHash()
	if worker.LastHash() != "" {
		t.Error("Expected empty hash after reset")
	}

	// Refresh should rebuild even though content unchanged
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	snapshot2 := worker.GetSnapshot()
	hash2 := worker.LastHash()

	// Hash should be repopulated
	if hash2 == "" {
		t.Error("Expected hash to be set after refresh")
	}

	// Should have rebuilt (new snapshot pointer)
	if snapshot1 == snapshot2 {
		t.Error("Expected new snapshot after hash reset")
	}
}

func TestBackgroundWorker_ForceRefreshBypassesDedup(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// Build initial snapshot and set hash.
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	snapshot1 := worker.GetSnapshot()
	if snapshot1 == nil {
		t.Fatal("Expected snapshot after initial refresh")
	}

	// Second refresh with same content should be deduped.
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)
	if worker.GetSnapshot() != snapshot1 {
		t.Fatal("Expected snapshot pointer to be unchanged after dedup")
	}

	// Force refresh should rebuild even though content is unchanged.
	worker.ForceRefresh()
	time.Sleep(200 * time.Millisecond)
	if worker.GetSnapshot() == snapshot1 {
		t.Fatal("Expected new snapshot after ForceRefresh")
	}
}

func TestBackgroundWorker_ForceRefreshRegeneratesBDExport(t *testing.T) {
	stale := `{"id":"STALE","title":"Stale export","status":"open","priority":1,"issue_type":"task"}` + "\n"
	first := `{"id":"FIRST","title":"First export","status":"open","priority":1,"issue_type":"task"}` + "\n"
	second := `{"id":"SECOND","title":"Second export","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, stale)
	payloadPath := installReloadFakeBD(t, root, first)

	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	worker.ForceSourceRefresh()
	waitForSnapshotVersion(t, worker, 1)
	waitForWorkerIdle(t, worker, 1)
	if snapshot := worker.GetSnapshot(); snapshot == nil || len(snapshot.Issues) != 1 || snapshot.Issues[0].ID != "FIRST" {
		t.Fatalf("first force refresh did not regenerate export: %#v", snapshot)
	}

	if err := os.WriteFile(payloadPath, []byte(second), 0o644); err != nil {
		t.Fatalf("change fake database payload: %v", err)
	}
	worker.ForceSourceRefresh()
	waitForSnapshotVersion(t, worker, 2)
	waitForWorkerIdle(t, worker, 2)
	if snapshot := worker.GetSnapshot(); snapshot == nil || len(snapshot.Issues) != 1 || snapshot.Issues[0].ID != "SECOND" {
		t.Fatalf("second force refresh used stale export: %#v", snapshot)
	}
}

func TestBackgroundWorker_ForceRefreshReportsBDExportFailure(t *testing.T) {
	stale := `{"id":"STALE","title":"Stale export","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, stale)

	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()
	if err := worker.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)
	waitForWorkerIdle(t, worker, 1)
	installFailingReloadFakeBD(t, root, "partial export\n")
	worker.ForceSourceRefresh()
	deadline := time.Now().Add(time.Second)
	for worker.State() != WorkerProcessing && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if worker.State() != WorkerProcessing {
		t.Fatal("worker did not begin source refresh")
	}
	worker.TriggerRefresh()

	msg := waitForBackgroundWorkerMsg(t, worker, time.Second, func(msg tea.Msg) bool {
		_, ok := msg.(SnapshotErrorMsg)
		return ok
	})
	reloadErr := msg.(SnapshotErrorMsg)
	if reloadErr.Err == nil || !strings.Contains(reloadErr.Err.Error(), "bd export failed") {
		t.Fatalf("expected explicit bd export error, got %v", reloadErr.Err)
	}
	time.Sleep(300 * time.Millisecond)
	if snapshot := worker.GetSnapshot(); snapshot == nil || len(snapshot.Issues) != 1 || snapshot.Issues[0].ID != "STALE" {
		t.Fatalf("failed export should retain previous snapshot: %#v", snapshot)
	}
	if got := worker.Metrics().SnapshotVersion; got != 1 {
		t.Fatalf("failed export triggered a stale watcher reload: snapshot version = %d", got)
	}
}

func TestBackgroundWorker_HubSignalRefreshesAndDeduplicates(t *testing.T) {
	stale := `{"id":"STALE","title":"Stale","status":"open","priority":1,"issue_type":"task"}` + "\n"
	fresh := `{"id":"FRESH","title":"Fresh","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, stale)
	payloadPath, countPath := installCountingReloadFakeBD(t, root, fresh)
	signalPath := filepath.Join(root, "viewer-generation")
	writeTestSignal(t, signalPath, "initial")

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:       issuesPath,
		HubChangeSignal: signalPath,
		DebounceDelay:   20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)

	writeTestSignal(t, signalPath, "changed")
	waitForSnapshotVersion(t, worker, 2)
	if snapshot := worker.GetSnapshot(); snapshot == nil || snapshot.Issues[0].ID != "FRESH" {
		t.Fatalf("Hub signal did not load fresh data: %#v", snapshot)
	}
	waitForFileLength(t, countPath, 1)

	// A real mutation signal still exports once, but unchanged issue data does
	// not replace the snapshot or trigger repeated analysis.
	writeTestSignal(t, signalPath, "unchanged")
	waitForFileLength(t, countPath, 2)
	waitForWorkerIdle(t, worker, 2)
	if got := worker.Metrics().SnapshotVersion; got != 2 {
		t.Fatalf("unchanged Hub data advanced snapshot version to %d", got)
	}
	if err := os.WriteFile(payloadPath, []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if got := fileLength(t, countPath); got != 2 {
		t.Fatalf("unchanged data caused repeated exports: %d", got)
	}
}

func TestBackgroundWorker_HubSignalBurstCoalesces(t *testing.T) {
	content := `{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, content)
	_, countPath := installCountingReloadFakeBD(t, root, content)
	signalPath := filepath.Join(root, "viewer-generation")
	writeTestSignal(t, signalPath, "initial")
	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:       issuesPath,
		HubChangeSignal: signalPath,
		DebounceDelay:   40 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)
	for i := 0; i < 8; i++ {
		writeTestSignal(t, signalPath, fmt.Sprintf("burst-%d", i))
	}
	waitForFileLength(t, countPath, 1)
	time.Sleep(100 * time.Millisecond)
	if got := fileLength(t, countPath); got != 1 {
		t.Fatalf("Hub signal burst produced %d exports, want 1", got)
	}
}

func TestBackgroundWorker_HubFailureRetainsSnapshotAndRecovers(t *testing.T) {
	stale := `{"id":"STALE","title":"Stale","status":"open","priority":1,"issue_type":"task"}` + "\n"
	fresh := `{"id":"FRESH","title":"Fresh","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, stale)
	payloadPath, _ := installCountingReloadFakeBD(t, root, fresh)
	installCountingFailingBD(t, payloadPath)
	signalPath := filepath.Join(root, "viewer-generation")
	writeTestSignal(t, signalPath, "initial")
	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:       issuesPath,
		HubChangeSignal: signalPath,
		DebounceDelay:   5 * time.Millisecond,
		SourceRetryBase: 80 * time.Millisecond,
		SourceRetryMax:  80 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Stop()
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)
	writeTestSignal(t, signalPath, "changed")
	waitForBackgroundWorkerMsg(t, worker, time.Second, func(msg tea.Msg) bool {
		_, ok := msg.(SnapshotErrorMsg)
		return ok
	})
	if snapshot := worker.GetSnapshot(); snapshot == nil || snapshot.Issues[0].ID != "STALE" {
		t.Fatalf("failed refresh replaced last valid snapshot: %#v", snapshot)
	}
	installCountingSuccessBD(t, payloadPath)
	waitForSnapshotVersion(t, worker, 2)
	if snapshot := worker.GetSnapshot(); snapshot == nil || snapshot.Issues[0].ID != "FRESH" {
		t.Fatalf("retry did not recover fresh data: %#v", snapshot)
	}
}

func TestBackgroundWorker_StopCancelsHubExport(t *testing.T) {
	content := `{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, content)
	startedPath := installBlockingReloadFakeBD(t, root)
	signalPath := filepath.Join(root, "viewer-generation")
	writeTestSignal(t, signalPath, "initial")
	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:       issuesPath,
		HubChangeSignal: signalPath,
		DebounceDelay:   5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(); err != nil {
		t.Fatal(err)
	}
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)
	writeTestSignal(t, signalPath, "changed")
	waitForFile(t, startedPath)

	stopped := make(chan struct{})
	go func() {
		worker.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not cancel the in-flight Hub export")
	}
	if worker.State() != WorkerStopped || worker.watcher.IsStarted() || worker.hubChangeWatcher.IsStarted() {
		t.Fatalf("worker or watcher remained active after Stop")
	}
}

func TestNewModel_HubAndLocalWatcherModes(t *testing.T) {
	content := `{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}` + "\n"
	t.Run("local", func(t *testing.T) {
		t.Setenv("BV_BACKGROUND_MODE", "0")
		t.Setenv("BV_HUB_CHANGE_SIGNAL", "")
		path := filepath.Join(t.TempDir(), "issues.jsonl")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		model := NewModel(nil, nil, path)
		defer model.Stop()
		if model.backgroundWorker != nil || model.watcher == nil {
			t.Fatalf("local mode watcher selection: worker=%v watcher=%v", model.backgroundWorker, model.watcher)
		}
	})

	t.Run("hub", func(t *testing.T) {
		t.Setenv("BV_BACKGROUND_MODE", "0")
		directory := t.TempDir()
		path := filepath.Join(directory, "issues.jsonl")
		signalPath := filepath.Join(directory, "viewer-generation")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("BV_HUB_CHANGE_SIGNAL", signalPath)
		model := NewModel(nil, nil, path)
		defer model.Stop()
		if model.backgroundWorker == nil || model.watcher != nil || model.backgroundWorker.hubChangeWatcher == nil {
			t.Fatalf("Hub mode watcher selection: worker=%v watcher=%v", model.backgroundWorker, model.watcher)
		}
	})
}

type startupSnapshotProbe struct {
	Model
	ready chan struct{}
}

func (m startupSnapshotProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.Model.Update(msg)
	m.Model = updated.(Model)
	if !m.snapshotInitPending && m.snapshot != nil {
		select {
		case <-m.ready:
		default:
			close(m.ready)
		}
		return m, tea.Quit
	}
	return m, cmd
}

func TestModelInitialHubSnapshotDoesNotRequireInput(t *testing.T) {
	content := `{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}` + "\n"
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	signalPath := filepath.Join(directory, "viewer-generation")
	if err := os.WriteFile(issuesPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestSignal(t, signalPath, "initial")

	t.Setenv("BV_BACKGROUND_MODE", "0")
	t.Setenv("BV_HUB_CHANGE_SIGNAL", signalPath)
	t.Setenv("BV_HUB_AUTO_REFRESH", "1")
	probe := startupSnapshotProbe{
		Model: NewModel([]model.Issue{{ID: "ONE", Title: "One", Status: model.StatusOpen, IssueType: model.TypeTask}}, nil, issuesPath),
		ready: make(chan struct{}),
	}
	defer probe.Model.Stop()
	if probe.snapshotInitPending || strings.Contains(probe.View(), "Loading beads") {
		t.Fatal("Hub auto-refresh hid already-loaded data behind the initial snapshot")
	}

	var output bytes.Buffer
	program := tea.NewProgram(probe, tea.WithInput(nil), tea.WithOutput(&output), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	select {
	case <-probe.ready:
	case <-time.After(2 * time.Second):
		program.Kill()
		<-done
		t.Fatal("initial Hub snapshot did not reach Update without user input")
	}
	if err := <-done; err != nil {
		t.Fatalf("Bubble Tea program failed: %v", err)
	}
	if !strings.Contains(output.String(), "One") {
		t.Fatal("initial Hub board was not rendered without user input")
	}
}

func TestModelEmptyInitialHubSnapshotLeavesLoadingWithoutInput(t *testing.T) {
	content := `{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}` + "\n"
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	signalPath := filepath.Join(directory, "viewer-generation")
	if err := os.WriteFile(issuesPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestSignal(t, signalPath, "initial")

	t.Setenv("BV_BACKGROUND_MODE", "0")
	t.Setenv("BV_HUB_CHANGE_SIGNAL", signalPath)
	t.Setenv("BV_HUB_AUTO_REFRESH", "1")
	probe := startupSnapshotProbe{
		Model: NewModel(nil, nil, issuesPath),
		ready: make(chan struct{}),
	}
	defer probe.Model.Stop()
	if !probe.snapshotInitPending || !strings.Contains(probe.View(), "Loading beads") {
		t.Fatal("empty background startup did not enter the loading state")
	}

	var output bytes.Buffer
	program := tea.NewProgram(probe, tea.WithInput(nil), tea.WithOutput(&output), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	select {
	case <-probe.ready:
	case <-time.After(2 * time.Second):
		program.Kill()
		<-done
		t.Fatal("empty Hub startup remained loading without user input")
	}
	if err := <-done; err != nil {
		t.Fatalf("Bubble Tea program failed: %v", err)
	}
	if !strings.Contains(output.String(), "One") {
		t.Fatal("empty Hub startup did not render its first board without user input")
	}
}

func TestModelHubAutoRefreshDisabledShowsLoadedDataImmediately(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	signalPath := filepath.Join(directory, "viewer-generation")
	if err := os.WriteFile(issuesPath, []byte(`{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("BV_BACKGROUND_MODE", "0")
	t.Setenv("BV_HUB_CHANGE_SIGNAL", signalPath)
	t.Setenv("BV_HUB_AUTO_REFRESH", "0")
	m := NewModel([]model.Issue{{ID: "ONE", Title: "One", Status: model.StatusOpen, IssueType: model.TypeTask}}, nil, issuesPath)
	defer m.Stop()

	if m.backgroundWorker != nil || m.snapshotInitPending {
		t.Fatalf("disabled Hub auto-refresh entered background loading: worker=%v pending=%v", m.backgroundWorker, m.snapshotInitPending)
	}
	if view := m.View(); strings.Contains(view, "Loading beads") {
		t.Fatal("disabled Hub auto-refresh hid already-loaded data behind loading screen")
	}
}

func TestModelBackgroundSnapshotCaptions(t *testing.T) {
	directory := t.TempDir()
	issuesPath := filepath.Join(directory, "issues.jsonl")
	signalPath := filepath.Join(directory, "viewer-generation")
	content := `{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(issuesPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestSignal(t, signalPath, "initial")

	t.Setenv("BV_BACKGROUND_MODE", "0")
	t.Setenv("BV_HUB_CHANGE_SIGNAL", signalPath)
	t.Setenv("BV_HUB_AUTO_REFRESH", "1")
	issues := []model.Issue{{ID: "ONE", Title: "One", Status: model.StatusOpen, IssueType: model.TypeTask}}
	m := NewModel(issues, nil, issuesPath)
	defer m.Stop()

	if got := m.statusMsg; got != "Background mode enabled" {
		t.Fatalf("initial status = %q, want %q", got, "Background mode enabled")
	}
	first, _ := m.Update(SnapshotReadyMsg{Snapshot: NewSnapshotBuilder(issues).Build()})
	m = first.(Model)
	if got := m.statusMsg; got != "Background mode enabled" {
		t.Fatalf("first snapshot status = %q, want %q", got, "Background mode enabled")
	}
	if !m.backgroundSnapshotApplied {
		t.Fatal("first background snapshot was not recorded")
	}

	second, _ := m.Update(SnapshotReadyMsg{Snapshot: NewSnapshotBuilder(issues).Build()})
	m = second.(Model)
	if got := m.statusMsg; got != "Reloaded 1 issues" {
		t.Fatalf("later snapshot status = %q, want %q", got, "Reloaded 1 issues")
	}
}

func installCountingReloadFakeBD(t *testing.T, root, payload string) (string, string) {
	t.Helper()
	payloadPath := installReloadFakeBD(t, root, payload)
	countPath := filepath.Join(filepath.Dir(payloadPath), "export-count")
	installCountingSuccessBD(t, payloadPath)
	return payloadPath, countPath
}

func installCountingSuccessBD(t *testing.T, payloadPath string) {
	t.Helper()
	binDir := filepath.Dir(payloadPath)
	countPath := filepath.Join(binDir, "export-count")
	script := "#!/bin/sh\nprintf x >> '" + countPath + "'\ncat '" + payloadPath + "' > \"$3\"\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func installCountingFailingBD(t *testing.T, payloadPath string) {
	t.Helper()
	binDir := filepath.Dir(payloadPath)
	countPath := filepath.Join(binDir, "export-count")
	script := "#!/bin/sh\nprintf x >> '" + countPath + "'\nprintf partial > \"$3\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func installBlockingReloadFakeBD(t *testing.T, root string) string {
	t.Helper()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	startedPath := filepath.Join(binDir, "started")
	script := "#!/bin/sh\nprintf started > '" + startedPath + "'\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return startedPath
}

func writeTestSignal(t *testing.T, path, generation string) {
	t.Helper()
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(generation), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
}

func fileLength(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(data)
}

func waitForFileLength(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && len(data) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s length %d", path, want)
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func TestBackgroundWorker_SetRecipe_RebuildsOnRecipeChangeWithSameName(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	waitForSnapshot := func(prev *DataSnapshot) *DataSnapshot {
		deadline := time.Now().Add(750 * time.Millisecond)
		for time.Now().Before(deadline) {
			snap := worker.GetSnapshot()
			if snap != nil && snap != prev {
				return snap
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for snapshot change (prev=%p)", prev)
		return nil
	}

	worker.TriggerRefresh()
	snap1 := waitForSnapshot(nil)

	r1 := &recipe.Recipe{
		Name: "demo",
		Filters: recipe.FilterConfig{
			Status: []string{"open"},
		},
	}
	worker.SetRecipe(r1)
	snap2 := waitForSnapshot(snap1)
	if snap2.RecipeName != "demo" {
		t.Fatalf("expected RecipeName demo, got %q", snap2.RecipeName)
	}
	if snap2.RecipeHash != recipeFingerprint(r1) {
		t.Fatalf("expected RecipeHash %q, got %q", recipeFingerprint(r1), snap2.RecipeHash)
	}

	// Same name, different filter: should still trigger a rebuild (bv-4ilb).
	r2 := &recipe.Recipe{
		Name: "demo",
		Filters: recipe.FilterConfig{
			Status: []string{"closed"},
		},
	}
	worker.SetRecipe(r2)
	snap3 := waitForSnapshot(snap2)
	if snap3.RecipeHash != recipeFingerprint(r2) {
		t.Fatalf("expected RecipeHash %q, got %q", recipeFingerprint(r2), snap3.RecipeHash)
	}
}

func TestBackgroundWorker_SetRecipeDoesNotRefreshBDExport(t *testing.T) {
	content := `{"id":"ONE","title":"One","status":"open","priority":1,"issue_type":"task"}` + "\n"
	root, issuesPath := makeReloadBDWorkspace(t, content)
	emptyPath := filepath.Join(root, "empty-bin")
	if err := os.Mkdir(emptyPath, 0o755); err != nil {
		t.Fatalf("create empty PATH: %v", err)
	}
	t.Setenv("PATH", emptyPath)

	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: issuesPath})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()
	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)
	waitForWorkerIdle(t, worker, 1)

	worker.SetRecipe(&recipe.Recipe{Name: "open"})
	waitForSnapshotVersion(t, worker, 2)
	waitForWorkerIdle(t, worker, 2)
	if snapshot := worker.GetSnapshot(); snapshot == nil || snapshot.RecipeName != "open" {
		t.Fatalf("recipe rebuild unexpectedly depended on bd export: %#v", snapshot)
	}
}

func TestBackgroundWorker_SnapshotHasDataHash(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	snapshot := worker.GetSnapshot()
	if snapshot == nil {
		t.Fatal("Expected snapshot")
	}

	// Snapshot should have DataHash populated
	if snapshot.DataHash == "" {
		t.Error("Expected DataHash to be set in snapshot")
	}

	// DataHash should match LastHash
	if snapshot.DataHash != worker.LastHash() {
		t.Errorf("DataHash mismatch: snapshot=%s, worker=%s", snapshot.DataHash, worker.LastHash())
	}
}

func TestBackgroundWorker_BuildSnapshotDoesNotPublishHashBeforeSwap(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content1 := `{"id":"test-1","title":"Initial","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content1), 0644); err != nil {
		t.Fatalf("Failed to write initial test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	worker.TriggerRefresh()
	waitForSnapshotVersion(t, worker, 1)

	accepted := worker.GetSnapshot()
	if accepted == nil {
		t.Fatal("expected accepted initial snapshot")
	}
	acceptedHash := worker.LastHash()
	if acceptedHash == "" {
		t.Fatal("expected accepted initial hash")
	}

	content2 := `{"id":"test-1","title":"Changed","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content2), 0644); err != nil {
		t.Fatalf("Failed to write changed test file: %v", err)
	}

	mutateWorkerForTest(worker, func() {
		worker.forceNext = true
	})

	unaccepted := worker.buildSnapshot(false, false)
	if unaccepted == nil {
		t.Fatal("expected unaccepted changed snapshot")
	}
	defer loader.ReturnIssuePtrsToPool(unaccepted.pooledIssues)

	if unaccepted.DataHash == "" {
		t.Fatal("expected unaccepted snapshot DataHash")
	}
	if unaccepted.DataHash == acceptedHash {
		t.Fatal("expected changed snapshot hash to differ from accepted hash")
	}
	if worker.GetSnapshot() != accepted {
		t.Fatal("buildSnapshot should not swap the active snapshot")
	}
	if got := worker.LastHash(); got != acceptedHash {
		t.Fatalf("LastHash changed before snapshot swap: got %q, want %q", got, acceptedHash)
	}
	worker.mu.RLock()
	forceNext := worker.forceNext
	worker.mu.RUnlock()
	if !forceNext {
		t.Fatal("buildSnapshot consumed a force-refresh flag that belongs to the next process run")
	}
}

func TestWorkerError_String(t *testing.T) {
	err := WorkerError{
		Phase:   "load",
		Cause:   os.ErrNotExist,
		Time:    time.Now(),
		Retries: 3,
	}

	s := err.Error()
	if s == "" {
		t.Error("Error() should return non-empty string")
	}

	if !strings.Contains(s, "load") {
		t.Errorf("Error() should contain phase 'load': %s", s)
	}

	if !strings.Contains(s, "3") {
		t.Errorf("Error() should contain retry count: %s", s)
	}

	// Test Unwrap
	if err.Unwrap() != os.ErrNotExist {
		t.Error("Unwrap() should return underlying error")
	}
}

func TestBackgroundWorker_LoadError(t *testing.T) {
	// Create a worker pointing to non-existent file
	cfg := WorkerConfig{
		BeadsPath:     "/nonexistent/path/beads.jsonl",
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		// Watcher creation might fail for non-existent path, which is fine
		t.Skipf("Skipping test - watcher creation failed: %v", err)
	}
	defer worker.Stop()

	// Trigger refresh
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	// Should have no snapshot (load failed)
	if worker.GetSnapshot() != nil {
		t.Error("Expected nil snapshot when file doesn't exist")
	}

	// Should have recorded error
	lastErr := worker.LastError()
	if lastErr == nil {
		t.Error("Expected error to be recorded")
	} else {
		if lastErr.Phase != "load" {
			t.Errorf("Expected phase 'load', got %q", lastErr.Phase)
		}
	}
}

func TestBackgroundWorker_ErrorRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	// Start with no file
	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// First refresh should fail (no file)
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	if worker.GetSnapshot() != nil {
		t.Error("Expected nil snapshot when file doesn't exist")
	}

	// Now create the file
	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Reset hash to force reload
	worker.ResetHash()

	// Second refresh should succeed
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	snapshot := worker.GetSnapshot()
	if snapshot == nil {
		t.Fatal("Expected snapshot after file created")
	}

	// Error should be cleared
	if worker.LastError() != nil {
		t.Error("Expected error to be cleared on success")
	}
}

func TestBackgroundWorker_SafeCompute(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// Test that safeCompute catches panics
	err2 := worker.safeCompute("test", func() error {
		panic("intentional panic for testing")
	})

	if err2 == nil {
		t.Error("safeCompute should catch panics")
	}

	if err2.Phase != "test" {
		t.Errorf("Expected phase 'test', got %q", err2.Phase)
	}

	// Verify worker still functional after panic
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	if worker.GetSnapshot() == nil {
		t.Error("Worker should still be functional after panic recovery")
	}
}

func TestHashPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "short string (empty hash)",
			input:    "empty",
			expected: "empty",
		},
		{
			name:     "exactly 16 chars",
			input:    "1234567890123456",
			expected: "1234567890123456",
		},
		{
			name:     "longer than 16 chars",
			input:    "8b423072ec4730921a2b3c4d5e6f7890",
			expected: "8b423072ec473092",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hashPrefix(tt.input)
			if result != tt.expected {
				t.Errorf("hashPrefix(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBackgroundWorker_StartAfterStop(t *testing.T) {
	// Test that Start() returns error after Stop() has been called
	cfg := WorkerConfig{
		BeadsPath: "", // No watcher needed for this test
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}

	// Start and stop the worker
	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	worker.Stop()

	// Attempting to start again should fail
	err = worker.Start()
	if err == nil {
		t.Error("Start() after Stop() should return an error")
	}

	// Verify the worker is stopped
	if worker.State() != WorkerStopped {
		t.Errorf("Expected WorkerStopped state, got %v", worker.State())
	}
}

func TestBackgroundWorker_ConcurrentTrigger(t *testing.T) {
	// Test that concurrent TriggerRefresh calls don't cause duplicate processing
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Fire multiple TriggerRefresh calls concurrently
	// The fix ensures only one process() runs at a time, others mark dirty
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			worker.TriggerRefresh()
		}(i)
	}
	wg.Wait()

	waitForSnapshotVersion(t, worker, 1)
	waitForWorkerIdle(t, worker, 1)

	// Worker should still be in idle state (not stuck in processing)
	if worker.State() != WorkerIdle {
		t.Errorf("Expected idle state after concurrent triggers, got %v", worker.State())
	}

	// Should have a valid snapshot
	if worker.GetSnapshot() == nil {
		t.Error("Expected snapshot after concurrent triggers")
	}
}

func TestBackgroundWorker_TriggerRefreshCoalescesWhileProcessScheduled(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{BeadsPath: ""})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	mutateWorkerForTest(worker, func() {
		worker.processScheduled = true
	})

	worker.TriggerRefresh()

	state, dirty, scheduled := workerStateFlags(worker)
	if state != WorkerIdle {
		t.Fatalf("state=%v, want %v", state, WorkerIdle)
	}
	if !scheduled {
		t.Fatal("expected existing scheduled process to remain scheduled")
	}
	if !dirty {
		t.Fatal("expected refresh to mark worker dirty while process is scheduled")
	}
	if got := worker.coalesceCount.Load(); got != 1 {
		t.Fatalf("coalesceCount=%d, want 1", got)
	}
	if got := worker.Metrics().ProcessingCount; got != 0 {
		t.Fatalf("ProcessingCount=%d, want 0", got)
	}
}

func TestBackgroundWorker_Phase2Async(t *testing.T) {
	// Test that Phase 2 analysis runs asynchronously (bv-e3ub)
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	// Create a file with some dependencies to make Phase 2 analysis non-trivial
	content := `{"id":"test-1","title":"Root","status":"open","priority":1,"issue_type":"task"}
{"id":"test-2","title":"Child","status":"open","priority":2,"issue_type":"task","dependencies":[{"depends_on":"test-1","type":"blocks"}]}
`
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// Trigger refresh and wait for snapshot
	worker.TriggerRefresh()
	time.Sleep(200 * time.Millisecond)

	snapshot := worker.GetSnapshot()
	if snapshot == nil {
		t.Fatal("Expected snapshot after refresh")
	}

	// Snapshot should exist with analysis
	if snapshot.Analysis == nil {
		t.Fatal("Expected Analysis in snapshot")
	}

	// Wait for Phase 2 to complete using the GraphStats API
	snapshot.Analysis.WaitForPhase2()

	// After waiting, Phase 2 should be ready
	if !snapshot.Analysis.IsPhase2Ready() {
		t.Error("Phase 2 should be ready after WaitForPhase2()")
	}
}

func TestBackgroundWorker_Phase2UpdateMsgDelivered(t *testing.T) {
	// Verify that the worker emits Phase2UpdateMsg asynchronously with a matching hash (bv-j97z).
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	// Small dependency graph; Phase 2 should typically run async.
	content := `{"id":"root","title":"Root","status":"open","priority":1,"issue_type":"task"}
{"id":"child","title":"Child","status":"open","priority":2,"issue_type":"task","dependencies":[{"depends_on_id":"root","type":"blocks"}]}
`
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 10 * time.Millisecond,
		MessageBuffer: 16,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	start := time.Now()
	t.Logf("[%s] EVENT=trigger_refresh", start.UTC().Format(time.RFC3339Nano))
	worker.TriggerRefresh()

	var snapshot *DataSnapshot
	var phase2Hash string
	var snapshotAt time.Time
	var phase2At time.Time

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	for snapshot == nil || phase2Hash == "" {
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for SnapshotReadyMsg and Phase2UpdateMsg (snapshot=%v, phase2Hash=%q)", snapshot != nil, phase2Hash)
		case msg := <-worker.Messages():
			switch m := msg.(type) {
			case SnapshotReadyMsg:
				if m.Snapshot == nil {
					continue
				}
				snapshot = m.Snapshot
				snapshotAt = time.Now()
				t.Logf("[%s] EVENT=snapshot_ready elapsed_ms=%.3f issues=%d hash=%s",
					snapshotAt.UTC().Format(time.RFC3339Nano),
					float64(snapshotAt.Sub(start).Microseconds())/1000.0,
					len(snapshot.Issues),
					hashPrefix(snapshot.DataHash),
				)

				// If Phase 2 already completed by the time we received the snapshot,
				// the update message may be suppressed (no work to signal).
				if snapshot.Analysis != nil && snapshot.Analysis.IsPhase2Ready() {
					t.Skip("phase 2 completed before snapshot delivery; Phase2UpdateMsg may not be emitted for this dataset")
				}

			case Phase2UpdateMsg:
				phase2Hash = m.DataHash
				phase2At = time.Now()
				t.Logf("[%s] EVENT=phase2_update elapsed_ms=%.3f hash=%s",
					phase2At.UTC().Format(time.RFC3339Nano),
					float64(phase2At.Sub(start).Microseconds())/1000.0,
					hashPrefix(phase2Hash),
				)
			}
		}
	}

	if snapshot.DataHash == "" {
		t.Fatal("expected snapshot DataHash to be set")
	}
	if phase2Hash != snapshot.DataHash {
		t.Fatalf("phase2 hash mismatch: got %s, want %s", phase2Hash, snapshot.DataHash)
	}
}

func TestBackgroundWorker_Phase2NoSendAfterStop(t *testing.T) {
	// Test that runPhase2Analysis doesn't send if worker is stopped (bv-e3ub)
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cfg := WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
	}

	worker, err := NewBackgroundWorker(cfg)
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}

	// Trigger refresh
	worker.TriggerRefresh()

	// Stop immediately (before Phase 2 can complete)
	worker.Stop()

	// Worker should be stopped
	if worker.State() != WorkerStopped {
		t.Errorf("Expected stopped state, got %v", worker.State())
	}

	// The test passes if we reach here without panicking
	// (runPhase2Analysis should gracefully handle stopped worker)
}

func TestDataSnapshot_GetGraphStats(t *testing.T) {
	// Test GetGraphStats helper method (bv-e3ub)

	// Test nil snapshot
	var nilSnapshot *DataSnapshot
	if nilSnapshot.GetGraphStats() != nil {
		t.Error("GetGraphStats on nil snapshot should return nil")
	}

	// Test snapshot with nil Analysis
	emptySnapshot := &DataSnapshot{}
	if emptySnapshot.GetGraphStats() != nil {
		t.Error("GetGraphStats with nil Analysis should return nil")
	}
}

func waitForBackgroundWorkerMsg(t *testing.T, worker *BackgroundWorker, timeout time.Duration, predicate func(tea.Msg) bool) tea.Msg {
	t.Helper()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case msg := <-worker.Messages():
			if predicate(msg) {
				return msg
			}
		case <-timer.C:
			t.Fatalf("timeout waiting for BackgroundWorker message (%v)", timeout)
		}
	}
}

func waitForSnapshotVersion(t *testing.T, worker *BackgroundWorker, minVersion uint64) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if worker.Metrics().SnapshotVersion >= minVersion {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for snapshot version %d (got %d)", minVersion, worker.Metrics().SnapshotVersion)
}

func waitForWorkerIdle(t *testing.T, worker *BackgroundWorker, minProcessingCount uint64) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		metrics := worker.Metrics()
		state, dirty, scheduled := workerStateFlags(worker)
		if state == WorkerIdle && !dirty && !scheduled && metrics.ProcessingCount >= minProcessingCount {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	metrics := worker.Metrics()
	state, dirty, scheduled := workerStateFlags(worker)
	t.Fatalf(
		"timeout waiting for idle worker after %d processes (state=%v dirty=%v scheduled=%v processes=%d snapshots=%d)",
		minProcessingCount,
		state,
		dirty,
		scheduled,
		metrics.ProcessingCount,
		metrics.SnapshotVersion,
	)
}

func workerStateFlags(worker *BackgroundWorker) (WorkerState, bool, bool) {
	worker.mu.RLock()
	defer worker.mu.RUnlock()
	return worker.state, worker.dirty, worker.processScheduled
}

func mutateWorkerForTest(worker *BackgroundWorker, fn func()) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	fn()
}

func TestBackgroundWorker_MalformedJSON_WarnsAndContinues(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"ok-1","title":"Ok 1","status":"open","priority":1,"issue_type":"task"}
not json
{"id":"bad-only-id"}
{"id":"ok-2","title":"Ok 2","status":"open","priority":2,"issue_type":"task"}
`
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	worker.TriggerRefresh()

	msg := waitForBackgroundWorkerMsg(t, worker, 2*time.Second, func(m tea.Msg) bool {
		_, ok := m.(SnapshotReadyMsg)
		return ok
	})

	ready := msg.(SnapshotReadyMsg)
	if ready.Snapshot == nil {
		t.Fatal("Expected non-nil snapshot")
	}
	if got, want := len(ready.Snapshot.Issues), 2; got != want {
		t.Fatalf("Expected %d issues, got %d", want, got)
	}
	if ready.Snapshot.LoadWarningCount == 0 {
		t.Error("Expected LoadWarningCount > 0 for malformed/invalid lines")
	}
	if worker.LastError() != nil {
		t.Errorf("Expected LastError to be nil for parse warnings, got: %v", worker.LastError())
	}
}

func TestBackgroundWorker_PreservesSnapshotOnPermissionErrorAndRecovers(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content1 := `{"id":"test-1","title":"Initial","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content1), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	// Build initial snapshot.
	worker.TriggerRefresh()
	msg1 := waitForBackgroundWorkerMsg(t, worker, 2*time.Second, func(m tea.Msg) bool {
		_, ok := m.(SnapshotReadyMsg)
		return ok
	})
	snapshot1 := msg1.(SnapshotReadyMsg).Snapshot
	if snapshot1 == nil {
		t.Fatal("Expected initial snapshot")
	}

	// Make file unreadable.
	if err := os.Chmod(beadsPath, 0000); err != nil {
		t.Skipf("chmod not supported: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(beadsPath, 0644)
	})

	worker.TriggerRefresh()
	msgErr := waitForBackgroundWorkerMsg(t, worker, 2*time.Second, func(m tea.Msg) bool {
		_, ok := m.(SnapshotErrorMsg)
		return ok
	})
	errMsg := msgErr.(SnapshotErrorMsg)
	if errMsg.Err == nil {
		t.Fatal("Expected SnapshotErrorMsg to contain error")
	}
	if !errMsg.Recoverable {
		t.Error("Expected Recoverable=true for permission errors")
	}

	// Snapshot must be preserved after an error.
	if worker.GetSnapshot() != snapshot1 {
		t.Fatal("Expected previous snapshot to be preserved on load error")
	}
	if worker.LastError() == nil {
		t.Fatal("Expected LastError to be set after load error")
	}

	// Restore permissions and write new content to force a successful rebuild.
	if err := os.Chmod(beadsPath, 0644); err != nil {
		t.Fatalf("Failed to restore file permissions: %v", err)
	}

	content2 := `{"id":"test-1","title":"Recovered","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content2), 0644); err != nil {
		t.Fatalf("Failed to write recovered file: %v", err)
	}
	worker.ResetHash()

	worker.TriggerRefresh()
	msg2 := waitForBackgroundWorkerMsg(t, worker, 2*time.Second, func(m tea.Msg) bool {
		_, ok := m.(SnapshotReadyMsg)
		return ok
	})
	snapshot2 := msg2.(SnapshotReadyMsg).Snapshot
	if snapshot2 == nil {
		t.Fatal("Expected snapshot after recovery")
	}
	if snapshot2 == snapshot1 {
		t.Fatal("Expected new snapshot pointer after recovery rebuild")
	}
	if got, want := snapshot2.Issues[0].Title, "Recovered"; got != want {
		t.Fatalf("Expected updated title %q, got %q", want, got)
	}
	if worker.LastError() != nil {
		t.Fatalf("Expected LastError to be cleared after recovery, got: %v", worker.LastError())
	}
}

func TestBackgroundWorker_HeartbeatUpdatesHealth(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:         beadsPath,
		DebounceDelay:     10 * time.Millisecond,
		HeartbeatInterval: 10 * time.Millisecond,
		HeartbeatTimeout:  200 * time.Millisecond,
		WatchdogInterval:  time.Hour, // keep deterministic in tests
		ProcessingTimeout: time.Hour,
		MaxRecoveries:     3,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	h1 := worker.Health()
	if !h1.Started || !h1.Alive || h1.LastHeartbeat.IsZero() {
		t.Fatalf("expected started+alive health, got: %+v", h1)
	}

	time.Sleep(30 * time.Millisecond)
	h2 := worker.Health()
	if !h2.LastHeartbeat.After(h1.LastHeartbeat) {
		t.Fatalf("expected heartbeat to advance: %v -> %v", h1.LastHeartbeat, h2.LastHeartbeat)
	}
}

func TestBackgroundWorker_CheckHealth_TriggersRecoveryOnMissedHeartbeat(t *testing.T) {
	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	content := `{"id":"test-1","title":"Test","status":"open","priority":1,"issue_type":"task"}` + "\n"
	if err := os.WriteFile(beadsPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:         beadsPath,
		DebounceDelay:     10 * time.Millisecond,
		HeartbeatInterval: time.Hour, // suppress updates so we can force "missed"
		HeartbeatTimeout:  10 * time.Millisecond,
		WatchdogInterval:  time.Hour, // keep deterministic in tests
		ProcessingTimeout: time.Hour,
		MaxRecoveries:     3,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	mutateWorkerForTest(worker, func() {
		worker.lastHeartbeat = time.Now().Add(-time.Second)
	})

	worker.checkHealth(time.Now())

	if got := worker.Health().RecoveryCount; got < 1 {
		t.Fatalf("expected recoveryCount to increment, got %d", got)
	}
	if worker.State() == WorkerStopped {
		t.Fatal("expected worker to remain running after recovery attempt")
	}
}

func TestBackgroundWorker_MaybeIdleGC_TriggersAfterThreshold(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath: "",
		IdleGC: &IdleGCConfig{
			Enabled:     true,
			Threshold:   5 * time.Second,
			CheckEvery:  time.Hour, // avoid nondeterministic ticker behavior in unit tests
			MinInterval: 30 * time.Second,
			GCPercent:   200,
		},
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	gcCalls := 0
	worker.idleGCFunc = func() { gcCalls++ }

	now := time.Now()
	worker.recordActivityAt(now.Add(-10 * time.Second))

	worker.maybeIdleGC(now)

	if gcCalls != 1 {
		t.Fatalf("expected idle GC to run once, ran %d times", gcCalls)
	}
	if got := worker.Health().IdleGCCount; got != 1 {
		t.Fatalf("expected IdleGCCount=1, got %d", got)
	}

	// Enforce min-interval gating.
	worker.maybeIdleGC(now.Add(1 * time.Second))
	if gcCalls != 1 {
		t.Fatalf("expected idle GC to be gated by MinInterval, ran %d times", gcCalls)
	}
}

func TestBackgroundWorker_MaybeIdleGC_DoesNotRunWhenProcessing(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath: "",
		IdleGC: &IdleGCConfig{
			Enabled:     true,
			Threshold:   5 * time.Second,
			CheckEvery:  time.Hour,
			MinInterval: 30 * time.Second,
			GCPercent:   200,
		},
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	gcCalls := 0
	worker.idleGCFunc = func() { gcCalls++ }

	now := time.Now()
	worker.recordActivityAt(now.Add(-10 * time.Second))

	mutateWorkerForTest(worker, func() {
		worker.state = WorkerProcessing
	})

	worker.maybeIdleGC(now)
	if gcCalls != 0 {
		t.Fatalf("expected idle GC to not run during processing, ran %d times", gcCalls)
	}
	if got := worker.Health().IdleGCCount; got != 0 {
		t.Fatalf("expected IdleGCCount=0, got %d", got)
	}
}

func TestBackgroundWorker_AttemptRecovery_GivesUpAfterMaxRecoveries(t *testing.T) {
	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:         "",
		MaxRecoveries:     1,
		HeartbeatInterval: time.Hour,
		WatchdogInterval:  time.Hour,
		HeartbeatTimeout:  10 * time.Millisecond,
		ProcessingTimeout: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	worker.attemptRecovery("test-1")

	worker.attemptRecovery("test-2")
	_ = waitForBackgroundWorkerMsg(t, worker, 2*time.Second, func(m tea.Msg) bool {
		msg, ok := m.(SnapshotErrorMsg)
		return ok && !msg.Recoverable
	}).(SnapshotErrorMsg)

	if worker.State() != WorkerStopped {
		t.Fatalf("expected worker to be stopped after giving up, got state=%v", worker.State())
	}
}

func TestStress_SustainedWrites(t *testing.T) {
	if os.Getenv("PERF_TEST") != "1" {
		t.Skip("set PERF_TEST=1 to run 10+ minute stress tests")
	}
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	const issueCount = 200
	if err := writeStressIssuesFile(beadsPath, issueCount, 0, "init"); err != nil {
		t.Fatalf("failed to write initial beads file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
		MessageBuffer: 16,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var snapshotCount atomic.Int64
	var errorCount atomic.Int64
	go countWorkerMessages(worker, &snapshotCount, &errorCount)

	var initialMem runtime.MemStats
	initialGoros := runtime.NumGoroutine()
	runtime.ReadMemStats(&initialMem)
	initialFDs, fdOK := procFDCount()

	duration := requireTestDurationOrSkip(t, 10*time.Minute, 30*time.Second)
	end := time.Now().Add(duration)
	writeInterval := 100 * time.Millisecond

	// Ensure the worker processes at least one file-change event before we start the long loop.
	if err := writeStressIssuesFile(beadsPath, issueCount, 0, "warmup"); err != nil {
		t.Fatalf("failed to write warmup beads file: %v", err)
	}
	waitForAtomicAtLeast(t, 10*time.Second, &snapshotCount, 1)

	writeCount := 0
	for now := time.Now(); now.Before(end); now = time.Now() {
		// Rewrite with stable issue count (stress file watching + parsing + analysis,
		// without unbounded memory growth from an ever-expanding dataset).
		changeIndex := writeCount % issueCount
		if err := writeStressIssuesFile(beadsPath, issueCount, changeIndex, fmt.Sprintf("tick-%d", writeCount)); err != nil {
			t.Fatalf("failed to write beads file: %v", err)
		}
		writeCount++

		// Sample every minute.
		if writeCount%600 == 0 {
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			goros := runtime.NumGoroutine()
			if fdOK {
				fds, _ := procFDCount()
				t.Logf("Minute %d: heap=%dMB goros=%d fds=%d writes=%d", writeCount/600, mem.Alloc/1024/1024, goros, fds, writeCount)
			} else {
				t.Logf("Minute %d: heap=%dMB goros=%d writes=%d", writeCount/600, mem.Alloc/1024/1024, goros, writeCount)
			}
		}

		time.Sleep(writeInterval)
	}

	worker.Stop()

	// Final checks.
	runtime.GC()
	time.Sleep(1 * time.Second)

	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)
	finalGoros := runtime.NumGoroutine()
	finalFDs := 0
	if fdOK {
		finalFDs, _ = procFDCount()
	}

	memDelta := int64(finalMem.Alloc) - int64(initialMem.Alloc)
	goroDelta := finalGoros - initialGoros
	fdDelta := finalFDs - initialFDs

	t.Logf("Final: heap=%dMB (delta=%dMB) goros=%d (delta=%d) fds=%d (delta=%d) writes=%d",
		finalMem.Alloc/1024/1024, memDelta/1024/1024,
		finalGoros, goroDelta,
		finalFDs, fdDelta,
		writeCount,
	)

	if got := snapshotCount.Load(); got < 1 {
		t.Fatalf("expected at least one SnapshotReadyMsg, got %d", got)
	}
	if got := errorCount.Load(); got != 0 {
		t.Fatalf("expected no SnapshotErrorMsg, got %d", got)
	}
	if goroDelta > 10 {
		t.Fatalf("goroutine leak: delta=%d (want <= 10)", goroDelta)
	}
	if memDelta > 100*1024*1024 {
		t.Fatalf("memory growth too high: delta=%dMB (want <= 100MB)", memDelta/1024/1024)
	}
	if fdOK && fdDelta > 10 {
		t.Fatalf("file descriptor leak: delta=%d (want <= 10)", fdDelta)
	}
}

func TestStress_BurstWrites(t *testing.T) {
	if os.Getenv("PERF_TEST") != "1" {
		t.Skip("set PERF_TEST=1 to run 10+ minute stress tests")
	}
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	const issueCount = 200
	if err := writeStressIssuesFile(beadsPath, issueCount, 0, "init"); err != nil {
		t.Fatalf("failed to write initial beads file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
		MessageBuffer: 16,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var snapshotCount atomic.Int64
	var errorCount atomic.Int64
	go countWorkerMessages(worker, &snapshotCount, &errorCount)

	var initialMem runtime.MemStats
	initialGoros := runtime.NumGoroutine()
	runtime.ReadMemStats(&initialMem)
	initialFDs, fdOK := procFDCount()

	duration := requireTestDurationOrSkip(t, 5*time.Minute, 30*time.Second)
	end := time.Now().Add(duration)

	writeCount := 0
	for time.Now().Before(end) {
		// Burst of 10 quick writes (agent completing task).
		for i := 0; i < 10; i++ {
			changeIndex := writeCount % issueCount
			if err := writeStressIssuesFile(beadsPath, issueCount, changeIndex, fmt.Sprintf("burst-%d", writeCount)); err != nil {
				t.Fatalf("failed to write beads file: %v", err)
			}
			writeCount++
			time.Sleep(10 * time.Millisecond)
		}

		// Quiet period (agent thinking).
		time.Sleep(2 * time.Second)
	}

	worker.Stop()
	runtime.GC()
	time.Sleep(1 * time.Second)

	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)
	finalGoros := runtime.NumGoroutine()
	finalFDs := 0
	if fdOK {
		finalFDs, _ = procFDCount()
	}

	memDelta := int64(finalMem.Alloc) - int64(initialMem.Alloc)
	goroDelta := finalGoros - initialGoros
	fdDelta := finalFDs - initialFDs

	t.Logf("Final: heap=%dMB (delta=%dMB) goros=%d (delta=%d) fds=%d (delta=%d) writes=%d snapshots=%d errors=%d",
		finalMem.Alloc/1024/1024, memDelta/1024/1024,
		finalGoros, goroDelta,
		finalFDs, fdDelta,
		writeCount,
		snapshotCount.Load(),
		errorCount.Load(),
	)

	if got := snapshotCount.Load(); got < 1 {
		t.Fatalf("expected at least one SnapshotReadyMsg, got %d", got)
	}
	if got := errorCount.Load(); got != 0 {
		t.Fatalf("expected no SnapshotErrorMsg, got %d", got)
	}
	if goroDelta > 10 {
		t.Fatalf("goroutine leak: delta=%d (want <= 10)", goroDelta)
	}
	if memDelta > 100*1024*1024 {
		t.Fatalf("memory growth too high: delta=%dMB (want <= 100MB)", memDelta/1024/1024)
	}
	if fdOK && fdDelta > 10 {
		t.Fatalf("file descriptor leak: delta=%d (want <= 10)", fdDelta)
	}
}

func TestStress_MemoryPressure(t *testing.T) {
	if os.Getenv("PERF_TEST") != "1" {
		t.Skip("set PERF_TEST=1 to run 10+ minute stress tests")
	}
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	// Simulate constrained memory environment.
	oldLimit := debug.SetMemoryLimit(256 * 1024 * 1024) // 256MB
	t.Cleanup(func() {
		debug.SetMemoryLimit(oldLimit)
	})

	tmpDir := t.TempDir()
	beadsPath := filepath.Join(tmpDir, "beads.jsonl")

	const issueCount = 2000
	if err := writeStressIssuesFile(beadsPath, issueCount, 0, "init"); err != nil {
		t.Fatalf("failed to write initial beads file: %v", err)
	}

	worker, err := NewBackgroundWorker(WorkerConfig{
		BeadsPath:     beadsPath,
		DebounceDelay: 50 * time.Millisecond,
		MessageBuffer: 16,
	})
	if err != nil {
		t.Fatalf("NewBackgroundWorker failed: %v", err)
	}
	defer worker.Stop()

	if err := worker.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	worker.TriggerRefresh()
	timeout := clampToDeadline(t, 60*time.Second, 30*time.Second)
	_ = waitForBackgroundWorkerMsg(t, worker, timeout, func(m tea.Msg) bool {
		msg, ok := m.(SnapshotReadyMsg)
		return ok && msg.Snapshot != nil
	})
}

func countWorkerMessages(worker *BackgroundWorker, snapshotCount, errorCount *atomic.Int64) {
	if worker == nil {
		return
	}
	for {
		select {
		case <-worker.Done():
			return
		case msg := <-worker.Messages():
			switch msg.(type) {
			case SnapshotReadyMsg:
				snapshotCount.Add(1)
			case SnapshotErrorMsg:
				errorCount.Add(1)
			}
		}
	}
}

func waitForAtomicAtLeast(t *testing.T, timeout time.Duration, counter *atomic.Int64, min int64) {
	t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		if counter.Load() >= min {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for counter >= %d (got %d)", min, counter.Load())
		case <-tick.C:
		}
	}
}

func requireTestDurationOrSkip(t *testing.T, desired, safetyWindow time.Duration) time.Duration {
	t.Helper()
	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < desired+safetyWindow {
			t.Skipf("need >= %s remaining before test deadline (have %s); run with -timeout >= %s", desired+safetyWindow, remaining, desired+safetyWindow)
		}
	}
	return desired
}

func clampToDeadline(t *testing.T, desired, safetyWindow time.Duration) time.Duration {
	t.Helper()
	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline) - safetyWindow
		if remaining <= 0 {
			t.Skip("insufficient time before test deadline; increase -timeout")
		}
		if remaining < desired {
			return remaining
		}
	}
	return desired
}

func procFDCount() (int, bool) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, false
	}
	return len(entries), true
}

func writeStressIssuesFile(path string, issueCount int, mutateIndex int, mutateSuffix string) error {
	if issueCount <= 0 {
		return fmt.Errorf("invalid issueCount: %d", issueCount)
	}
	if mutateIndex < 0 || mutateIndex >= issueCount {
		return fmt.Errorf("invalid mutateIndex: %d", mutateIndex)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; i < issueCount; i++ {
		title := fmt.Sprintf("Stress Issue %d", i)
		if i == mutateIndex {
			title = fmt.Sprintf("%s (%s)", title, mutateSuffix)
		}

		// Keep the payload small and stable (stress parsing/analysis without inflating memory).
		// created_at / updated_at are optional (zero values are accepted), but including updated_at
		// forces content to change while remaining valid JSON.
		line := fmt.Sprintf(
			`{"id":"stress-%d","title":%q,"status":"open","priority":1,"issue_type":"task","updated_at":%q}`+"\n",
			i, title, now,
		)
		if _, err := f.WriteString(line); err != nil {
			return err
		}
	}

	return f.Sync()
}
