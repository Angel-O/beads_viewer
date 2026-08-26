package agents

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var errAgentFileChanged = errors.New("agent file changed while the bv edit was being prepared")

type lockedAgentFile struct {
	path    string
	file    *os.File
	info    os.FileInfo
	content []byte
}

// lockAgentFileForMutation opens and locks the exact inode whose bytes will be
// transformed. The lock serializes cooperating bv processes; the identity and
// byte checks in replace also reject editors that ignore advisory locks and
// save the path before the final rename.
func lockAgentFileForMutation(filePath string) (*lockedAgentFile, error) {
	file, err := openAgentFileForMutation(filePath)
	if err != nil {
		return nil, err
	}
	if err := lockAgentFile(file); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock file: %w", err)
	}

	beforeInfo, beforeStatErr := file.Stat()
	content, readErr := io.ReadAll(file)
	afterInfo, afterStatErr := file.Stat()
	if beforeStatErr != nil || readErr != nil || afterStatErr != nil {
		_ = unlockAgentFile(file)
		_ = file.Close()
		if beforeStatErr != nil {
			return nil, beforeStatErr
		}
		if readErr != nil {
			return nil, readErr
		}
		return nil, afterStatErr
	}
	if !sameAgentFileSnapshot(beforeInfo, afterInfo) || int64(len(content)) != afterInfo.Size() {
		_ = unlockAgentFile(file)
		_ = file.Close()
		return nil, errAgentFileChanged
	}

	return &lockedAgentFile{path: filePath, file: file, info: afterInfo, content: content}, nil
}

func (f *lockedAgentFile) close() {
	if f == nil || f.file == nil {
		return
	}
	_ = unlockAgentFile(f.file)
	_ = f.file.Close()
	f.file = nil
}

func (f *lockedAgentFile) verifyUnchanged() error {
	current, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("%w: reopen destination: %v", errAgentFileChanged, err)
	}
	defer current.Close()

	currentInfo, err := current.Stat()
	if err != nil {
		return fmt.Errorf("%w: stat destination: %v", errAgentFileChanged, err)
	}
	if !os.SameFile(f.info, currentInfo) {
		return fmt.Errorf("%w: destination identity was replaced", errAgentFileChanged)
	}
	if f.info.Mode() != currentInfo.Mode() {
		return fmt.Errorf("%w: destination mode changed from %s to %s", errAgentFileChanged, f.info.Mode(), currentInfo.Mode())
	}
	content, err := io.ReadAll(current)
	if err != nil {
		return fmt.Errorf("%w: reread destination: %v", errAgentFileChanged, err)
	}
	afterReadInfo, err := current.Stat()
	if err != nil || !sameAgentFileSnapshot(currentInfo, afterReadInfo) || int64(len(content)) != afterReadInfo.Size() {
		return fmt.Errorf("%w: destination changed while it was being verified", errAgentFileChanged)
	}
	if !bytes.Equal(f.content, content) {
		return fmt.Errorf("%w: destination bytes changed", errAgentFileChanged)
	}
	pathInfo, err := os.Stat(f.path)
	if err != nil || !sameAgentFileSnapshot(afterReadInfo, pathInfo) {
		return fmt.Errorf("%w: destination changed during verification", errAgentFileChanged)
	}
	return nil
}

func sameAgentFileSnapshot(a, b os.FileInfo) bool {
	return a != nil && b != nil &&
		os.SameFile(a, b) &&
		a.Size() == b.Size() &&
		a.Mode() == b.Mode() &&
		a.ModTime().Equal(b.ModTime())
}

func (f *lockedAgentFile) replace(content []byte) error {
	return writeVerifiedReplacement(f.path, content, f.info.Mode(), f.verifyUnchanged)
}

// AppendBlurbToFile appends the agent blurb to the specified file.
// The complete result is validated before a same-directory replacement so a
// blurb cannot be written inside an EOF-terminated Markdown fence.
func AppendBlurbToFile(filePath string) error {
	locked, err := lockAgentFileForMutation(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	defer locked.close()

	contentStr := string(locked.content)
	if _, err := inspectBlurbStructure(contentStr); err != nil {
		return fmt.Errorf("validate existing blurb markers: %w", err)
	}
	if ContainsAnyBlurb(contentStr) {
		return fmt.Errorf("agent file already contains bv instructions; update or remove them instead")
	}

	// Append blurb using the string function
	newContent := AppendBlurb(contentStr)
	count, err := inspectBlurbStructure(newContent)
	if err != nil {
		return fmt.Errorf("validate appended blurb: %w", err)
	}
	if count != 1 || GetBlurbVersion(newContent) != BlurbVersion {
		return fmt.Errorf("validate appended blurb: found %d standalone versioned blocks at v%d, want exactly one v%d block", count, GetBlurbVersion(newContent), BlurbVersion)
	}

	if err := locked.replace([]byte(newContent)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// UpdateBlurbInFile replaces an existing blurb with the current version.
// Uses a fully written same-directory replacement to prevent partial writes.
func UpdateBlurbInFile(filePath string) error {
	locked, err := lockAgentFileForMutation(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	defer locked.close()

	newContent, err := updateBlurbChecked(string(locked.content))
	if err != nil {
		return fmt.Errorf("validate existing blurb: %w", err)
	}

	if err := locked.replace([]byte(newContent)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// RemoveBlurbFromFile removes all versioned and legacy agent blurbs from the
// specified file. Malformed and future-version markers are rejected without
// writing. Uses a fully written same-directory replacement.
func RemoveBlurbFromFile(filePath string) error {
	locked, err := lockAgentFileForMutation(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	defer locked.close()

	newContent, err := removeBlurbsChecked(string(locked.content))
	if err != nil {
		return fmt.Errorf("validate existing blurb: %w", err)
	}
	if newContent == string(locked.content) {
		return nil
	}

	if err := locked.replace([]byte(newContent)); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// CreateAgentFile creates a new AGENTS.md file with the blurb content.
// The file is created exclusively with standard permissions (0644); an
// existing path is never replaced, including if it appears after detection.
func CreateAgentFile(filePath string) error {
	content := "# AI Agent Instructions\n\n" + AgentBlurb + "\n"
	if err := writeNewFileExclusive(filePath, []byte(content)); err != nil {
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
	version := GetBlurbVersion(contentStr)
	if version != BlurbVersion {
		return false, fmt.Errorf("validate blurb version: found v%d, want current v%d", version, BlurbVersion)
	}
	return true, nil
}

func writeNewFileExclusive(filePath string, content []byte) error {
	return writeNewFileExclusiveUsing(filePath, content, os.Link)
}

func writeNewFileExclusiveUsing(filePath string, content []byte, link func(string, string) error) error {
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, ".bv-create-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(content); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	// Linking a fully written same-directory temp file creates the destination
	// name in one operation and fails with os.ErrExist instead of replacing it.
	if err := link(tmpPath, filePath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("link new file: %w", err)
		}
		// Hard links are unavailable on common filesystems such as exFAT and
		// FAT32. Fall back to O_EXCL so portability never weakens the
		// no-replacement guarantee.
		if fallbackErr := writeFileDirectExclusive(filePath, content); fallbackErr != nil {
			return fmt.Errorf("link new file: %v; exclusive create fallback: %w", err, fallbackErr)
		}
	}
	return nil
}

func writeFileDirectExclusive(filePath string, content []byte) error {
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("open destination exclusively: %w", err)
	}
	createdInfo, statErr := file.Stat()
	closed := false
	success := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		if success {
			return
		}
		// O_EXCL proves the path did not pre-exist. Remove a partial file only
		// while the destination still names the exact file this call created.
		if pathInfo, statErr := os.Stat(filePath); statErr == nil && createdInfo != nil && os.SameFile(createdInfo, pathInfo) {
			_ = os.Remove(filePath)
		}
	}()
	if statErr != nil {
		return fmt.Errorf("stat new destination: %w", statErr)
	}

	if err := file.Chmod(0o644); err != nil {
		return fmt.Errorf("chmod destination: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync destination: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}
	closed = true
	success = true
	return nil
}

// writeVerifiedReplacement writes a complete same-directory temp file, calls
// verify immediately before commit, then renames over the destination. The
// caller supplies the mode captured from the locked source inode. No mutation
// path is allowed to skip the stale-source check.
func writeVerifiedReplacement(filePath string, content []byte, mode os.FileMode, verify func() error) error {
	if verify == nil {
		return fmt.Errorf("replacement requires stale-source verification")
	}
	// Create the temp file in the same directory so replacement stays on one
	// filesystem and Unix can provide atomic rename semantics.
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, ".bv-replace-*")
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

	if err := verify(); err != nil {
		return fmt.Errorf("verify destination before replacement: %w", err)
	}

	// os.Rename supports replacement on Windows as well as Unix. Do not add a
	// remove-then-rename fallback: that creates a data-loss window.
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	success = true
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
