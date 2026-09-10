package repository

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepositoryIdentity returns the canonical worktree root and Git common
// directory for the repository containing dir. The common directory gives
// linked worktrees one stable identity for application-owned caches.
func RepositoryIdentity(dir string) (string, string, error) {
	root, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return "", "", errors.New("cannot resolve the current Git repository root")
	}
	root, err = canonicalDirectory(root)
	if err != nil {
		return "", "", fmt.Errorf("cannot canonicalize the current Git repository root: %w", err)
	}
	common, err := gitOutput(root, "rev-parse", "--git-common-dir")
	if err != nil || common == "" {
		return "", "", errors.New("cannot resolve the Git common directory")
	}
	if strings.ContainsAny(common, "\n\r\t") {
		return "", "", errors.New("Git common directory contains an unsupported control character")
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	common, err = canonicalDirectory(common)
	if err != nil {
		return "", "", fmt.Errorf("cannot canonicalize Git common directory: %w", err)
	}
	return root, common, nil
}

func canonicalDirectory(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", resolved)
	}
	return resolved, nil
}

func gitOutput(dir string, arguments ...string) (string, error) {
	args := append([]string{"-C", dir}, arguments...)
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(output), "\n"), nil
}
