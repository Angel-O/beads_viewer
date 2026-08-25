package correlation

import (
	"bytes"
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

	type externalBatchRequest struct {
		context    string
		repository string
		commits    []string
	}
	type loadedCommit struct {
		commit CorrelatedCommit
		err    error
	}
	requestsByContext := make(map[string]*externalBatchRequest)
	for _, correlation := range applicable {
		if _, skip := unavailable[correlation.Context]; skip {
			continue
		}
		request := requestsByContext[correlation.Context]
		if request == nil {
			request = &externalBatchRequest{context: correlation.Context, repository: hub.repositories[correlation.Context]}
			requestsByContext[correlation.Context] = request
		}
		seen := false
		for _, requested := range request.commits {
			if strings.EqualFold(requested, correlation.Commit) {
				seen = true
				break
			}
		}
		if !seen {
			request.commits = append(request.commits, correlation.Commit)
		}
	}
	for _, request := range requestsByContext {
		sort.SliceStable(request.commits, func(i, j int) bool {
			return strings.ToLower(request.commits[i]) < strings.ToLower(request.commits[j])
		})
	}

	loaded := make(map[string]loadedCommit)
	for _, contextKey := range repositoryKeys {
		request := requestsByContext[contextKey]
		if request == nil {
			continue
		}
		batch, batchErr := c.loadExternalCommits(request.context, request.repository, request.commits)
		for _, requested := range request.commits {
			identity := repositoryCommitIdentity(request.context, strings.ToLower(requested))
			if batchErr != nil {
				loaded[identity] = loadedCommit{err: batchErr}
				continue
			}
			result := batch[strings.ToLower(requested)]
			loaded[identity] = loadedCommit{commit: result.commit, err: result.err}
		}
	}

	commits := make([]CorrelatedCommit, 0, len(applicable))
	for _, correlation := range applicable {
		if _, skip := unavailable[correlation.Context]; skip {
			continue
		}
		identity := repositoryCommitIdentity(correlation.Context, strings.ToLower(correlation.Commit))
		entry, exists := loaded[identity]
		if !exists {
			return nil, fmt.Errorf("correlation ledger %q record %d: commit %q was not included in the repository batch", hub.ledger, correlation.record, correlation.Commit)
		}
		if entry.err != nil {
			return nil, fmt.Errorf("correlation ledger %q record %d: %w", hub.ledger, correlation.record, entry.err)
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

type externalCommitMetadata struct {
	sha        string
	author     string
	authorMail string
	timestamp  time.Time
	message    string
}

type externalCommitResult struct {
	commit CorrelatedCommit
	err    error
}

func (c *Correlator) loadExternalCommits(repository, repoPath string, requested []string) (map[string]externalCommitResult, error) {
	commits := make(map[string]externalCommitResult, len(requested))
	resolved, perCommitErr, err := c.resolveExternalCommits(repository, repoPath, requested)
	if err != nil {
		return commits, err
	}
	resolvedSHAs := make([]string, 0, len(resolved))
	seenResolved := make(map[string]struct{}, len(resolved))
	for _, requestedSHA := range requested {
		if sha := resolved[strings.ToLower(requestedSHA)]; sha != "" {
			sha = strings.ToLower(sha)
			if _, seen := seenResolved[sha]; seen {
				continue
			}
			seenResolved[sha] = struct{}{}
			resolvedSHAs = append(resolvedSHAs, sha)
		}
	}
	if len(resolvedSHAs) == 0 {
		for _, requestedSHA := range requested {
			key := strings.ToLower(requestedSHA)
			commits[key] = externalCommitResult{err: perCommitErr[key]}
		}
		return commits, nil
	}
	metadata, err := c.batchExternalCommitMetadata(repoPath, resolvedSHAs)
	if err != nil {
		return commits, err
	}
	extractor := NewCoCommitExtractor(repoPath)
	extractor.ctx = c.ctx
	filesBySHA, filesErr := extractor.batchFilesChangedWithRenameDetection(resolvedSHAs)
	if filesErr != nil {
		return commits, fmt.Errorf("loading changed paths from repository %q: %w", repository, filesErr)
	}
	statsBySHA, statsErr := extractor.batchLineStatsWithRenameDetection(resolvedSHAs)
	if statsErr != nil {
		return commits, fmt.Errorf("loading line statistics from repository %q: %w", repository, statsErr)
	}
	for _, requestedSHA := range requested {
		key := strings.ToLower(requestedSHA)
		if resolveErr := perCommitErr[key]; resolveErr != nil {
			commits[key] = externalCommitResult{err: resolveErr}
			continue
		}
		resolvedSHA := resolved[key]
		if resolvedSHA == "" {
			commits[key] = externalCommitResult{err: fmt.Errorf("commit %q was not resolved in repository %q", requestedSHA, repository)}
			continue
		}
		meta, ok := metadata[resolvedSHA]
		if !ok {
			return commits, fmt.Errorf("loading commit %q metadata from repository %q: unexpected Git output", resolvedSHA, repository)
		}
		files := filesBySHA[resolvedSHA]
		stats := statsBySHA[resolvedSHA]
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
		commits[key] = externalCommitResult{commit: CorrelatedCommit{
			Repository:  repository,
			SHA:         meta.sha,
			ShortSHA:    shortSHA(meta.sha),
			Message:     meta.message,
			Author:      meta.author,
			AuthorEmail: meta.authorMail,
			Timestamp:   meta.timestamp,
			Files:       filtered,
			Method:      MethodExternalLedger,
			Confidence:  1,
			Reason:      "Explicit Beads Hub correlation",
		}}
	}
	return commits, nil
}

func (c *Correlator) resolveExternalCommits(repository, repoPath string, requested []string) (map[string]string, map[string]error, error) {
	resolved := make(map[string]string, len(requested))
	perCommitErr := make(map[string]error)
	cmd := gitCommand(c.ctx, "cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	cmd.Dir = repoPath
	queries := make([]string, len(requested))
	for i, requestedSHA := range requested {
		queries[i] = requestedSHA + "^{commit}"
	}
	cmd.Stdin = strings.NewReader(strings.Join(queries, "\n") + "\n")
	out, err := runGitOutputBounded(cmd)
	if err != nil {
		if c.ctx != nil && c.ctx.Err() != nil {
			return resolved, perCommitErr, c.ctx.Err()
		}
		return resolved, perCommitErr, fmt.Errorf("resolving commits in repository %q: %w", repoPath, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != len(requested) {
		return resolved, perCommitErr, fmt.Errorf("resolving commits in repository %q: unexpected Git output: got %d results for %d commits", repoPath, len(lines), len(requested))
	}
	for i, line := range lines {
		parts := strings.Fields(line)
		key := strings.ToLower(requested[i])
		if len(parts) == 2 && parts[1] == "missing" {
			perCommitErr[key] = fmt.Errorf("commit %q is absent from repository %q at %q: not found", requested[i], repository, repoPath)
			continue
		}
		if len(parts) != 3 || parts[1] != "commit" {
			perCommitErr[key] = fmt.Errorf("commit %q is not a commit in repository %q at %q: %s", requested[i], repository, repoPath, line)
			continue
		}
		resolved[key] = strings.ToLower(parts[0])
	}
	return resolved, perCommitErr, nil
}

func (c *Correlator) batchExternalCommitMetadata(repoPath string, shas []string) (map[string]externalCommitMetadata, error) {
	metadata := make(map[string]externalCommitMetadata, len(shas))
	if len(shas) == 0 {
		return metadata, nil
	}
	args := make([]string, 0, len(shas)+4)
	args = append(args, "log", "--no-walk=unsorted", "--format=%x00%H%x00%aI%x00%an%x00%ae%x00%B%x00")
	args = append(args, shas...)
	cmd := gitCommand(c.ctx, withNoColorGit(args)...)
	cmd.Dir = repoPath
	out, err := runGitOutputBounded(cmd)
	if err != nil {
		if c.ctx != nil && c.ctx.Err() != nil {
			return metadata, c.ctx.Err()
		}
		return metadata, fmt.Errorf("loading commit metadata from repository %q: %w", repoPath, err)
	}
	for offset := 0; offset < len(out); {
		if out[offset] != 0 {
			return metadata, fmt.Errorf("loading commit metadata from repository %q: unexpected Git output", repoPath)
		}
		offset++
		fields := make([][]byte, 5)
		for i := range fields {
			end := bytes.IndexByte(out[offset:], 0)
			if end < 0 {
				return metadata, fmt.Errorf("loading commit metadata from repository %q: unexpected Git output", repoPath)
			}
			end += offset
			fields[i] = out[offset:end]
			offset = end + 1
		}
		if !validExternalSHA(fields[0]) {
			return metadata, fmt.Errorf("loading commit metadata from repository %q: unexpected Git output", repoPath)
		}
		sha := string(fields[0])
		timestamp, parseErr := time.Parse(time.RFC3339, string(fields[1]))
		if parseErr != nil {
			return metadata, fmt.Errorf("parsing commit %q timestamp from repository %q: %w", sha, repoPath, parseErr)
		}
		if _, duplicate := metadata[sha]; duplicate {
			return metadata, fmt.Errorf("loading commit metadata from repository %q: duplicate record for %q", repoPath, sha)
		}
		metadata[sha] = externalCommitMetadata{
			sha:        sha,
			author:     string(fields[2]),
			authorMail: string(fields[3]),
			timestamp:  timestamp,
			message:    strings.TrimSpace(string(fields[4])),
		}
		if offset == len(out) {
			break
		}
		if out[offset] != '\n' {
			return metadata, fmt.Errorf("loading commit metadata from repository %q: unexpected Git output", repoPath)
		}
		offset++
	}
	if len(metadata) != len(shas) {
		return metadata, fmt.Errorf("loading commit metadata from repository %q: unexpected Git output: got %d records for %d commits", repoPath, len(metadata), len(shas))
	}
	return metadata, nil
}

func validExternalSHA(value []byte) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
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
