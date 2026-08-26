package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AppendBlurbToFile appends the agent blurb to the specified file.
// Uses atomic write to prevent corruption.
func AppendBlurbToFile(filePath string) error {
	// Read existing content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	contentStr := string(content)
	if _, err := inspectBlurbStructure(contentStr); err != nil {
		return fmt.Errorf("validate existing blurb markers: %w", err)
	}
	if ContainsAnyBlurb(contentStr) {
		return fmt.Errorf("agent file already contains bv instructions; update or remove them instead")
	}

	// Append blurb using the string function
	newContent := AppendBlurb(contentStr)

	// Write atomically
	if err := atomicWrite(filePath, []byte(newContent)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// UpdateBlurbInFile replaces an existing blurb with the current version.
// Uses atomic write to prevent corruption.
func UpdateBlurbInFile(filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	newContent, err := updateBlurbChecked(string(content))
	if err != nil {
		return fmt.Errorf("validate existing blurb: %w", err)
	}

	if err := atomicWrite(filePath, []byte(newContent)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// RemoveBlurbFromFile removes all versioned and legacy agent blurbs from the
// specified file. Malformed versioned markers are rejected without writing.
// Uses atomic write to prevent corruption.
func RemoveBlurbFromFile(filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	newContent, err := removeBlurbsChecked(string(content))
	if err != nil {
		return fmt.Errorf("validate existing blurb: %w", err)
	}
	if newContent == string(content) {
		return nil
	}

	if err := atomicWrite(filePath, []byte(newContent)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// CreateAgentFile creates a new AGENTS.md file with the blurb content.
// The file is created with standard permissions (0644).
func CreateAgentFile(filePath string) error {
	// Create with just the blurb (no existing content)
	content := "# AI Agent Instructions\n\n" + AgentBlurb + "\n"

	// Write atomically
	if err := atomicWrite(filePath, []byte(content)); err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	return nil
}

// VerifyBlurbPresent checks that exactly one structurally valid versioned blurb
// is present and that no legacy blurb remains.
func VerifyBlurbPresent(filePath string) (bool, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}
	contentStr := string(content)
	count, err := inspectBlurbStructure(contentStr)
	if err != nil {
		return false, fmt.Errorf("validate blurb structure: %w", err)
	}
	if count == 0 {
		return false, nil
	}
	if count > 1 {
		return false, fmt.Errorf("validate blurb structure: found %d versioned blurb blocks, want exactly 1", count)
	}
	if ContainsLegacyBlurb(contentStr) {
		return false, fmt.Errorf("validate blurb structure: legacy blurb remains alongside versioned blurb")
	}
	return true, nil
}

// atomicWrite writes content to a file atomically using a temp file and rename.
// This prevents partial writes from corrupting the original file.
func atomicWrite(filePath string, content []byte) error {
	// Get file info to preserve permissions
	var mode os.FileMode = 0644
	if info, err := os.Stat(filePath); err == nil {
		mode = info.Mode()
	}

	// Create temp file in same directory (required for atomic rename)
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, ".bv-atomic-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Cleanup temp file on error
	success := false
	defer func() {
		if !success {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()

	// Write content
	if _, err := tmp.Write(content); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	// Ensure data is flushed to disk
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Set permissions on temp file
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, filePath); err != nil {
		// Windows does not allow renaming over an existing file.
		if runtime.GOOS == "windows" {
			if info, statErr := os.Lstat(filePath); statErr == nil && !info.IsDir() {
				if replaceErr := replaceFileWithBackup(tmpPath, filePath); replaceErr != nil {
					return replaceErr
				}
				success = true
				return nil
			}
		}
		return fmt.Errorf("rename temp file: %w", err)
	}

	success = true
	return nil
}

// replaceFileWithBackup provides the recoverable replacement path needed on
// Windows, where os.Rename cannot replace an existing file. The original is
// moved intact to a unique same-directory backup before the new file is
// installed. If installation fails, the original name is restored; if even
// that rollback fails, the error reports the preserved backup path.
func replaceFileWithBackup(tmpPath, filePath string) error {
	backupPath := tmpPath + ".backup"
	if _, err := os.Lstat(backupPath); err == nil {
		return fmt.Errorf("reserve backup path %s: path already exists", backupPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("check backup path %s: %w", backupPath, err)
	}

	if err := os.Rename(filePath, backupPath); err != nil {
		return fmt.Errorf("secure original file as backup: %w", err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		if restoreErr := os.Rename(backupPath, filePath); restoreErr != nil {
			return fmt.Errorf("install replacement: %w; restore original: %v; original preserved at %s", err, restoreErr, backupPath)
		}
		return fmt.Errorf("install replacement: %w (original restored)", err)
	}

	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("replacement installed but remove backup %s: %w", backupPath, err)
	}
	return nil
}

// EnsureBlurb ensures the blurb is present in an agent file.
// If the file exists without blurb, appends it.
// If the file has an old version, updates it.
// If the file doesn't exist, creates it.
func EnsureBlurb(workDir string) error {
	detection := DetectAgentFile(workDir)

	if !detection.Found() {
		// No agent file exists - create one
		filePath := GetPreferredAgentFilePath(workDir)
		return CreateAgentFile(filePath)
	}

	if detection.NeedsBlurb() {
		// File exists but no blurb - append
		return AppendBlurbToFile(detection.FilePath)
	}

	if detection.NeedsUpgrade() {
		// File has old blurb - update
		return UpdateBlurbInFile(detection.FilePath)
	}

	// Already has current blurb
	return nil
}
