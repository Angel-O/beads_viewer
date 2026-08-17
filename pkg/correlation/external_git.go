package correlation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (c *Correlator) extractExternalHistoryArtifact(beads []BeadInfo, opts CorrelatorOptions) (*historyArtifact, error) {
	hub, err := loadHubConfig(c.hubConfigPath, beads)
	if err != nil {
		return nil, err
	}

	type applicableCorrelation struct {
		ExternalHistoryCorrelation
		record int
	}
	applicable := make([]applicableCorrelation, 0, len(hub.correlations))
	skippedByContext := make(map[string]int)
	for i, correlation := range hub.correlations {
		if opts.BeadID != "" && correlation.BeadID != opts.BeadID {
			continue
		}
		applicable = append(applicable, applicableCorrelation{ExternalHistoryCorrelation: correlation, record: i + 1})
		skippedByContext[correlation.Context]++
	}

	repositoryKeys := make([]string, 0, len(skippedByContext))
	for key := range skippedByContext {
		repositoryKeys = append(repositoryKeys, key)
	}
	sort.Strings(repositoryKeys)
	unavailable := make(map[string]struct{})
	warnings := make([]HistoryWarning, 0)
	for _, key := range repositoryKeys {
		path := hub.repositories[key]
		reason, probeErr := c.probeExternalRepository(path)
		if probeErr != nil {
			return nil, fmt.Errorf("probing external history repository %q: %w", key, probeErr)
		}
		if reason != "" {
			unavailable[key] = struct{}{}
			warnings = append(warnings, HistoryWarning{
				Code:                HistoryWarningExternalRepositoryUnavailable,
				Context:             key,
				Reason:              reason,
				SkippedCorrelations: skippedByContext[key],
				Message:             fmt.Sprintf("Source history for context %q is unavailable; correlations from that context were skipped.", key),
			})
		}
	}

	type loadedCommit struct {
		commit CorrelatedCommit
	}
	loaded := make(map[string]loadedCommit)
	commits := make([]CorrelatedCommit, 0, len(applicable))
	for _, correlation := range applicable {
		if _, skip := unavailable[correlation.Context]; skip {
			continue
		}
		identity := repositoryCommitIdentity(correlation.Context, strings.ToLower(correlation.Commit))
		entry, exists := loaded[identity]
		if !exists {
			commit, loadErr := c.loadExternalCommit(correlation.Context, hub.repositories[correlation.Context], correlation.Commit)
			if loadErr != nil {
				return nil, fmt.Errorf("correlation ledger %q record %d: %w", hub.ledger, correlation.record, loadErr)
			}
			entry = loadedCommit{commit: commit}
			loaded[identity] = entry
		}
		commit := entry.commit
		if opts.Since != nil && commit.Timestamp.Before(*opts.Since) {
			continue
		}
		if opts.Until != nil && commit.Timestamp.After(*opts.Until) {
			continue
		}
		commit.BeadID = correlation.BeadID
		commits = append(commits, commit)
	}

	sort.SliceStable(commits, func(i, j int) bool {
		if commits[i].Timestamp.Equal(commits[j].Timestamp) {
			return repositoryCommitIdentity(commits[i].Repository, commits[i].SHA) < repositoryCommitIdentity(commits[j].Repository, commits[j].SHA)
		}
		return commits[i].Timestamp.Before(commits[j].Timestamp)
	})
	if opts.Limit > 0 {
		selected := make(map[string]struct{}, opts.Limit)
		for i := len(commits) - 1; i >= 0 && len(selected) < opts.Limit; i-- {
			selected[repositoryCommitIdentity(commits[i].Repository, commits[i].SHA)] = struct{}{}
		}
		filtered := commits[:0]
		for _, commit := range commits {
			if _, ok := selected[repositoryCommitIdentity(commit.Repository, commit.SHA)]; ok {
				filtered = append(filtered, commit)
			}
		}
		commits = filtered
	}
	lifecycleBeads := selectLifecycleBeads(beads, hub.correlations, opts.BeadID)
	events, err := loadBeadsLifecycle(c.ctx, hub.store, lifecycleBeads, opts)
	if err != nil {
		return nil, err
	}
	return &historyArtifact{Events: events, Commits: commits, Warnings: warnings}, nil
}

func (c *Correlator) probeExternalRepository(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "not_found", nil
		}
		return "unreadable", nil
	}
	if !info.IsDir() {
		return "not_directory", nil
	}

	cmd := gitCommand(c.ctx, "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.CombinedOutput()
	if c.ctx != nil && c.ctx.Err() != nil {
		return "", c.ctx.Err()
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", fmt.Errorf("Git executable unavailable: %w", err)
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return "", fmt.Errorf("Git executable unavailable: %w", err)
	}
	if err != nil {
		metadataPath := filepath.Join(path, ".git")
		metadataInfo, metadataErr := os.Lstat(metadataPath)
		if !errors.Is(metadataErr, os.ErrNotExist) {
			if metadataErr != nil || gitMetadataUnreadable(metadataPath, metadataInfo) {
				return "unreadable", nil
			}
			return "", fmt.Errorf("validating Git checkout metadata: %w", err)
		}
		return "not_git", nil
	}
	if strings.TrimSpace(string(out)) != "true" {
		return "not_git", nil
	}
	return "", nil
}

func gitMetadataUnreadable(metadataPath string, info os.FileInfo) bool {
	if info.IsDir() {
		metadataPath = filepath.Join(metadataPath, "HEAD")
	}
	_, err := os.ReadFile(metadataPath)
	return errors.Is(err, os.ErrPermission)
}

func selectLifecycleBeads(beads []BeadInfo, correlations []ExternalHistoryCorrelation, selectedBeadID string) []BeadInfo {
	wanted := make(map[string]struct{})
	if selectedBeadID != "" {
		wanted[selectedBeadID] = struct{}{}
	} else {
		for _, correlation := range correlations {
			wanted[correlation.BeadID] = struct{}{}
		}
	}
	selected := make([]BeadInfo, 0, len(wanted))
	for _, bead := range beads {
		if _, ok := wanted[bead.ID]; ok {
			selected = append(selected, bead)
		}
	}
	return selected
}

func (c *Correlator) loadExternalCommit(repository, repoPath, requestedSHA string) (CorrelatedCommit, error) {
	resolve := gitCommand(c.ctx, "-C", repoPath, "rev-parse", "--verify", requestedSHA+"^{commit}")
	resolvedOut, err := resolve.CombinedOutput()
	if err != nil {
		if c.ctx != nil && c.ctx.Err() != nil {
			return CorrelatedCommit{}, c.ctx.Err()
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return CorrelatedCommit{}, err
		}
		return CorrelatedCommit{}, fmt.Errorf("commit %q is absent from repository %q at %q: %s", requestedSHA, repository, repoPath, strings.TrimSpace(string(resolvedOut)))
	}
	resolvedSHA := strings.TrimSpace(string(resolvedOut))

	metadata := gitCommand(c.ctx, "-C", repoPath, "show", "-s", "--format=%H%x00%an%x00%ae%x00%aI%x00%B", resolvedSHA)
	metadataOut, err := metadata.Output()
	if err != nil {
		return CorrelatedCommit{}, fmt.Errorf("loading commit %q metadata from repository %q: %w", resolvedSHA, repository, err)
	}
	parts := bytes.SplitN(metadataOut, []byte{0}, 5)
	if len(parts) != 5 {
		return CorrelatedCommit{}, fmt.Errorf("loading commit %q metadata from repository %q: unexpected Git output", resolvedSHA, repository)
	}
	timestamp, err := time.Parse(time.RFC3339, strings.TrimSpace(string(parts[3])))
	if err != nil {
		return CorrelatedCommit{}, fmt.Errorf("parsing commit %q timestamp from repository %q: %w", resolvedSHA, repository, err)
	}

	extractor := NewCoCommitExtractor(repoPath)
	extractor.ctx = c.ctx
	files, err := extractor.getFilesChanged(resolvedSHA)
	if err != nil {
		return CorrelatedCommit{}, fmt.Errorf("loading commit %q changed paths from repository %q: %w", resolvedSHA, repository, err)
	}
	stats, err := extractor.getLineStats(resolvedSHA)
	if err != nil {
		return CorrelatedCommit{}, fmt.Errorf("loading commit %q line statistics from repository %q: %w", resolvedSHA, repository, err)
	}
	filtered := make([]FileChange, 0, len(files))
	for _, file := range files {
		if isExcludedPath(file.Path) {
			continue
		}
		file.Repository = repository
		if stat, exists := stats[file.Path]; exists {
			file.Insertions = stat.insertions
			file.Deletions = stat.deletions
		}
		filtered = append(filtered, file)
	}

	return CorrelatedCommit{
		Repository:  repository,
		SHA:         strings.TrimSpace(string(parts[0])),
		ShortSHA:    shortSHA(resolvedSHA),
		Message:     strings.TrimSpace(string(parts[4])),
		Author:      string(parts[1]),
		AuthorEmail: string(parts[2]),
		Timestamp:   timestamp,
		Files:       filtered,
		Method:      MethodExternalLedger,
		Confidence:  1,
		Reason:      "Explicit Beads Hub correlation",
	}, nil
}

func repositoryCommitIdentity(repository, sha string) string {
	if repository == "" {
		return sha
	}
	return repository + ":" + sha
}

// CommitIdentity returns the repository-aware key used by reverse indexes.
func CommitIdentity(commit CorrelatedCommit) string {
	return repositoryCommitIdentity(commit.Repository, commit.SHA)
}

func repositoryFileIdentity(repository, path string) string {
	if repository == "" {
		return path
	}
	return repository + ":" + path
}
