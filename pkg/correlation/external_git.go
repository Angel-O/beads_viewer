package correlation

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

func (c *Correlator) extractExternalHistoryArtifact(beads []BeadInfo, opts CorrelatorOptions) (*historyArtifact, error) {
	hub, err := loadHubConfig(c.hubConfigPath, beads)
	if err != nil {
		return nil, err
	}

	repositoryKeys := make([]string, 0, len(hub.repositories))
	for key := range hub.repositories {
		repositoryKeys = append(repositoryKeys, key)
	}
	sort.Strings(repositoryKeys)
	for _, key := range repositoryKeys {
		path := hub.repositories[key]
		info, statErr := os.Stat(path)
		if statErr != nil {
			return nil, fmt.Errorf("external history repository %q at %q is unreadable: %w", key, path, statErr)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("external history repository %q path %q is not a directory", key, path)
		}
		cmd := gitCommand(c.ctx, "-C", path, "rev-parse", "--is-inside-work-tree")
		out, commandErr := cmd.CombinedOutput()
		if commandErr != nil || strings.TrimSpace(string(out)) != "true" {
			return nil, fmt.Errorf("external history repository %q at %q is not a readable Git checkout: %s", key, path, strings.TrimSpace(string(out)))
		}
	}

	type loadedCommit struct {
		commit CorrelatedCommit
	}
	loaded := make(map[string]loadedCommit)
	commits := make([]CorrelatedCommit, 0, len(hub.correlations))
	for i, correlation := range hub.correlations {
		if opts.BeadID != "" && correlation.BeadID != opts.BeadID {
			continue
		}
		identity := repositoryCommitIdentity(correlation.Context, strings.ToLower(correlation.Commit))
		entry, exists := loaded[identity]
		if !exists {
			commit, loadErr := c.loadExternalCommit(correlation.Context, hub.repositories[correlation.Context], correlation.Commit)
			if loadErr != nil {
				return nil, fmt.Errorf("correlation ledger %q record %d: %w", hub.ledger, i+1, loadErr)
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
	return &historyArtifact{Events: events, Commits: commits}, nil
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
