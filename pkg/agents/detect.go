package agents

import (
	"os"
	"path/filepath"
)

// AgentFileDetection contains the result of detecting an agent config file.
type AgentFileDetection struct {
	// FilePath is the full path to the found file (empty if none found)
	FilePath string

	// FileType is the type of file found ("AGENTS.md", "CLAUDE.md", etc.)
	FileType string

	// HasBlurb indicates whether the file contains a versioned-marker prefix or
	// a detected legacy blurb. Consult BlurbStructureError before mutation.
	HasBlurb bool

	// HasLegacyBlurb indicates the file has the old-format blurb (pre-v1, no HTML markers)
	HasLegacyBlurb bool

	// BlurbVersion is the highest version found (0 if none or legacy).
	BlurbVersion int

	// BlurbCount is the number of complete, structurally valid versioned blocks
	// found before any malformed marker. A healthy injected blurb has count 1.
	BlurbCount int

	// BlurbStructureError describes malformed versioned marker structure. It is
	// empty when all versioned start/end markers are balanced and non-nested.
	BlurbStructureError string

	// Content is the file content (populated if file was read)
	Content string
}

// Found returns true if an agent file was detected.
func (d AgentFileDetection) Found() bool {
	return d.FilePath != ""
}

// NeedsBlurb returns true if the file exists but doesn't have our blurb.
func (d AgentFileDetection) NeedsBlurb() bool {
	return d.Found() && !d.HasBlurb && !d.HasMalformedBlurb()
}

// HasMalformedBlurb reports whether versioned marker structure is invalid.
func (d AgentFileDetection) HasMalformedBlurb() bool {
	return d.BlurbStructureError != ""
}

// HasDuplicateBlurbs reports whether more than one complete versioned block is
// present. Duplicate current-version blocks still need normalization.
func (d AgentFileDetection) HasDuplicateBlurbs() bool {
	return d.BlurbCount > 1
}

// HasFutureBlurb reports that the file contains instructions written by a
// newer bv binary. Older binaries must never normalize or downgrade them.
func (d AgentFileDetection) HasFutureBlurb() bool {
	return d.HasBlurb && d.BlurbVersion > BlurbVersion
}

// NeedsUpgrade returns true when the blurb needs repair or normalization:
// malformed markers, duplicate versioned blocks, legacy content, or an older
// versioned blurb all require attention.
func (d AgentFileDetection) NeedsUpgrade() bool {
	if d.HasMalformedBlurb() || d.HasDuplicateBlurbs() || d.HasLegacyBlurb {
		return true
	}
	return d.HasBlurb && d.BlurbVersion != BlurbVersion
}

// DetectAgentFile looks for AGENTS.md or CLAUDE.md in the given directory.
// It checks AGENTS.md first (preferred), then falls back to CLAUDE.md.
// The function reads the file content to check for existing blurb markers.
func DetectAgentFile(workDir string) AgentFileDetection {
	// Try each supported file in order of preference
	for _, filename := range SupportedAgentFiles {
		// Only check uppercase variants first (AGENTS.md, CLAUDE.md)
		if filename[0] < 'A' || filename[0] > 'Z' {
			continue
		}

		filePath := filepath.Join(workDir, filename)
		if detection := checkAgentFile(filePath, filename); detection.Found() {
			return detection
		}
	}

	// Try lowercase variants as fallback
	for _, filename := range SupportedAgentFiles {
		if filename[0] >= 'A' && filename[0] <= 'Z' {
			continue
		}

		filePath := filepath.Join(workDir, filename)
		if detection := checkAgentFile(filePath, filename); detection.Found() {
			return detection
		}
	}

	return AgentFileDetection{}
}

// checkAgentFile checks a specific file path for agent configuration.
func checkAgentFile(filePath, fileType string) AgentFileDetection {
	// Check if file exists
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return AgentFileDetection{}
	}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		// File exists but not readable - return detection without content
		return AgentFileDetection{
			FilePath: filePath,
			FileType: fileType,
		}
	}

	contentStr := string(content)
	hasLegacy := ContainsLegacyBlurb(contentStr)
	blurbCount, structureErr := inspectBlurbStructure(contentStr)
	structureError := ""
	if structureErr != nil {
		structureError = structureErr.Error()
	}

	return AgentFileDetection{
		FilePath:            filePath,
		FileType:            fileType,
		HasBlurb:            ContainsAnyBlurb(contentStr),
		HasLegacyBlurb:      hasLegacy,
		BlurbVersion:        GetBlurbVersion(contentStr),
		BlurbCount:          blurbCount,
		BlurbStructureError: structureError,
		Content:             contentStr,
	}
}

// DetectAgentFileInParents searches for agent files starting from workDir
// and walking up the directory tree. This is useful for finding a project-level
// AGENTS.md when running from a subdirectory.
// maxLevels limits how many parent directories to check (0 = current only).
func DetectAgentFileInParents(workDir string, maxLevels int) AgentFileDetection {
	currentDir := workDir
	for i := 0; i <= maxLevels; i++ {
		if detection := DetectAgentFile(currentDir); detection.Found() {
			return detection
		}

		// Move to parent directory
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			// Reached root
			break
		}
		currentDir = parentDir
	}

	return AgentFileDetection{}
}

// AgentFileExists checks if any supported agent file exists in the directory.
// This is a quick check without reading file content.
func AgentFileExists(workDir string) bool {
	for _, filename := range SupportedAgentFiles {
		filePath := filepath.Join(workDir, filename)
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// GetPreferredAgentFilePath returns the path where a new agent file should be created.
// It returns the path for AGENTS.md (preferred format).
func GetPreferredAgentFilePath(workDir string) string {
	return filepath.Join(workDir, "AGENTS.md")
}
