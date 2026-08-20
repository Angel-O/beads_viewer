package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
)

func normalizeRepoKey(raw string) string {
	key := strings.TrimSpace(raw)
	key = strings.TrimRight(key, "-:_")
	key = strings.ToLower(key)
	if key == "" || key == "." || strings.ContainsAny(key, `/\`) {
		return ""
	}
	return key
}

// normalizeRepoPrefixes normalizes workspace repo prefixes (e.g., "api-" -> "api")
// for display and interactive filtering.
func normalizeRepoPrefixes(prefixes []string) []string {
	if len(prefixes) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(prefixes))
	var out []string
	for _, raw := range prefixes {
		p := normalizeRepoKey(raw)
		if p == "" {
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func workspaceRepositoryCatalog(prefixes []string, repositories []WorkspaceRepositoryInfo, issues []model.Issue) model.RepositoryCatalog {
	counts := make(map[string]int, len(prefixes))
	for _, issue := range issues {
		if key := issueRepoKey(issue); key != "" {
			counts[key]++
		}
	}
	catalog := make(model.RepositoryCatalog, 0, len(prefixes))
	metadata := make(map[string]WorkspaceRepositoryInfo, len(repositories))
	for _, repository := range repositories {
		if key := normalizeRepoKey(repository.Prefix); key != "" {
			metadata[key] = repository
		}
	}
	for _, key := range normalizeRepoPrefixes(prefixes) {
		repository := metadata[key]
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			name = key
		}
		catalog = append(catalog, model.RepositoryCatalogEntry{
			ID:        key,
			Name:      name,
			Path:      repository.Path,
			Detail:    repository.Path,
			BeadCount: counts[key],
			Kind:      model.RepositoryIdentityWorkspacePrefix,
		})
	}
	model.SortRepositoryCatalog(catalog)
	return catalog
}

func issueRepoKey(issue model.Issue) string {
	if key := normalizeRepoKey(issue.SourceRepo); key != "" {
		return key
	}
	return normalizeRepoKey(ExtractRepoPrefix(issue.ID))
}

func sortedRepoKeys(selected map[string]bool) []string {
	if len(selected) == 0 {
		return nil
	}
	out := make([]string, 0, len(selected))
	for k, v := range selected {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// formatRepoList formats a sorted list of repo keys, truncating after maxNames.
// Example: ["api","web","lib"] with maxNames=2 -> "api,web+1".
func formatRepoList(repos []string, maxNames int) string {
	if len(repos) == 0 {
		return ""
	}
	if maxNames <= 0 {
		return fmt.Sprintf("%d repos", len(repos))
	}
	if len(repos) <= maxNames {
		return strings.Join(repos, ",")
	}
	head := strings.Join(repos[:maxNames], ",")
	return fmt.Sprintf("%s+%d", head, len(repos)-maxNames)
}
