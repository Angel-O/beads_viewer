package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Dicklesworthstone/beads_viewer/pkg/model"
	repositorypkg "github.com/Dicklesworthstone/beads_viewer/pkg/repository"
	"gopkg.in/yaml.v3"
)

func testRepositoryLabelPredicate(label string) bool { return !strings.HasPrefix(label, "ctx:") }

// testRepositoryCatalogLoader keeps UI tests independent from Hub policy and
// exercises only the neutral catalog contract they consume.
func testRepositoryCatalogLoader(path string, issues []model.Issue) (repositorypkg.Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config struct {
		Repositories map[string]struct {
			Path string `yaml:"path"`
		} `yaml:"repositories"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, issue := range issues {
		seen := make(map[string]bool)
		for _, label := range issue.Labels {
			if _, ok := config.Repositories[label]; ok && !seen[label] {
				counts[label]++
				seen[label] = true
			}
		}
	}
	catalog := make(repositorypkg.Catalog, 0, len(config.Repositories))
	for id, entry := range config.Repositories {
		catalog = append(catalog, repositorypkg.CatalogEntry{
			ID: id, Name: filepath.Base(entry.Path), Path: entry.Path,
			Detail: entry.Path, BeadCount: counts[id], Kind: repositorypkg.IdentityExact,
		})
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].ID < catalog[j].ID })
	return catalog, nil
}
