package agents

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendBlurbToFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")

	// Create file with initial content
	initial := "# My AGENTS.md\n\nSome existing content."
	if err := os.WriteFile(filePath, []byte(initial), 0644); err != nil {
		t.Fatal(err)
	}

	// Append blurb
	if err := AppendBlurbToFile(filePath); err != nil {
		t.Fatal(err)
	}

	// Verify
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "Some existing content.") {
		t.Error("Original content was not preserved")
	}
	if !strings.Contains(contentStr, BlurbStartMarker) {
		t.Error("Blurb start marker not found")
	}
	if !strings.Contains(contentStr, BlurbEndMarker) {
		t.Error("Blurb end marker not found")
	}
}

func TestAppendBlurbToEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")

	// Create empty file
	if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	// Append blurb
	if err := AppendBlurbToFile(filePath); err != nil {
		t.Fatal(err)
	}

	// Verify
	present, err := VerifyBlurbPresent(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Error("Blurb should be present")
	}
}

func TestAppendBlurbToFileRejectsMalformedMarkersWithoutWriting(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")
	original := "# Header\n\n<!-- end-bv-agent-instructions -->\nUser instructions"
	if err := os.WriteFile(filePath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AppendBlurbToFile(filePath); err == nil {
		t.Fatal("expected malformed marker error")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("malformed append changed file:\n got: %q\nwant: %q", content, original)
	}
}

func TestUpdateBlurbInFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")

	// Create file with old blurb (simulated)
	oldContent := "# My AGENTS.md\n\n<!-- bv-agent-instructions-v1 -->\nOld content\n<!-- end-bv-agent-instructions -->\n"
	if err := os.WriteFile(filePath, []byte(oldContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Update blurb
	if err := UpdateBlurbInFile(filePath); err != nil {
		t.Fatal(err)
	}

	// Verify - should have new blurb, only one copy
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	count := strings.Count(contentStr, BlurbStartMarker)
	if count != 1 {
		t.Errorf("Expected exactly 1 blurb marker, got %d", count)
	}
	if !strings.Contains(contentStr, "br ready") {
		t.Error("Updated blurb should contain current content")
	}
}

func TestUpdateBlurbInFileRejectsMalformedMarkersWithoutWriting(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")
	original := "# Header\n\n<!-- bv-agent-instructions-v1 -->\nUser instructions without an end marker"
	if err := os.WriteFile(filePath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := UpdateBlurbInFile(filePath); err == nil {
		t.Fatal("expected malformed marker error")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("malformed update changed file:\n got: %q\nwant: %q", content, original)
	}

	if err := UpdateBlurbInFile(filePath); err == nil {
		t.Fatal("expected repeated malformed marker error")
	}
	content, err = os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("repeated malformed update changed file:\n got: %q\nwant: %q", content, original)
	}
}

func TestUpdateBlurbInFileRejectsFutureVersionWithoutWriting(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")
	original := "# Header\n\n<!-- bv-agent-instructions-v9 -->\nnewer\n<!-- end-bv-agent-instructions -->\n"
	if err := os.WriteFile(filePath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	err := UpdateBlurbInFile(filePath)
	if err == nil || !strings.Contains(err.Error(), "refusing to replace") {
		t.Fatalf("UpdateBlurbInFile() error=%v, want future-version refusal", err)
	}
	got, readErr := os.ReadFile(filePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Fatalf("future-version update changed file:\n got: %q\nwant: %q", got, original)
	}
}

func TestRemoveBlurbFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")

	// Create file with blurb
	content := "# My AGENTS.md\n\n" + AgentBlurb + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Remove blurb
	if err := RemoveBlurbFromFile(filePath); err != nil {
		t.Fatal(err)
	}

	// Verify
	newContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(newContent), BlurbStartMarker) {
		t.Error("Blurb should have been removed")
	}
	if !strings.Contains(string(newContent), "# My AGENTS.md") {
		t.Error("Header should still be present")
	}
}

func TestRemoveBlurbFromFileRemovesLegacyAndDuplicateVersionedBlocks(t *testing.T) {
	t.Run("legacy", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "AGENTS.md")
		original := `# Header

### Using bv as an AI sidecar

--robot-insights
--robot-plan
bv already computes the hard parts for you.

## Preserve Me
`
		if err := os.WriteFile(filePath, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		if err := RemoveBlurbFromFile(filePath); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), "bv already computes the hard parts") {
			t.Fatalf("legacy blurb was not removed:\n%s", content)
		}
		if !strings.Contains(string(content), "# Header") || !strings.Contains(string(content), "## Preserve Me") {
			t.Fatalf("legacy removal lost surrounding content:\n%s", content)
		}
	})

	t.Run("multiple versioned", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "AGENTS.md")
		original := "# Header\n\n" +
			"<!-- bv-agent-instructions-v4 -->\none\n<!-- end-bv-agent-instructions -->\n\n" +
			"Preserve between.\n\n" +
			"<!-- bv-agent-instructions-v4 -->\ntwo\n<!-- end-bv-agent-instructions -->\n\n# Footer\n"
		if err := os.WriteFile(filePath, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		if err := RemoveBlurbFromFile(filePath); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), blurbStartPrefix) || strings.Contains(string(content), BlurbEndMarker) {
			t.Fatalf("not all versioned blurbs were removed:\n%s", content)
		}
		for _, preserved := range []string{"# Header", "Preserve between.", "# Footer"} {
			if !strings.Contains(string(content), preserved) {
				t.Fatalf("versioned removal lost %q:\n%s", preserved, content)
			}
		}
	})
}

func TestRemoveBlurbFromFileRejectsMalformedMarkersWithoutWriting(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")
	original := "# Header\n<!-- bv-agent-instructions-v1 -->\nUser instructions\n" +
		"<!-- bv-agent-instructions-v4 -->\nMore user instructions\n<!-- end-bv-agent-instructions -->\n# Footer"
	if err := os.WriteFile(filePath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	if err := RemoveBlurbFromFile(filePath); err == nil {
		t.Fatal("expected malformed marker error")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("malformed removal changed file:\n got: %q\nwant: %q", content, original)
	}
}

func TestCreateAgentFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")

	// Create new file
	if err := CreateAgentFile(filePath); err != nil {
		t.Fatal(err)
	}

	// Verify file exists with blurb
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "# AI Agent Instructions") {
		t.Error("Expected header")
	}
	if !strings.Contains(contentStr, BlurbStartMarker) {
		t.Error("Expected blurb marker")
	}
}

func TestCreateAgentFileDoesNotReplaceExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "AGENTS.md")
	original := []byte("# Existing instructions\n\nDo not overwrite me.\n")
	if err := os.WriteFile(filePath, original, 0o600); err != nil {
		t.Fatal(err)
	}

	err := CreateAgentFile(filePath)
	if err == nil || !errors.Is(err, os.ErrExist) {
		t.Fatalf("CreateAgentFile() error=%v, want os.ErrExist", err)
	}
	got, readErr := os.ReadFile(filePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("CreateAgentFile() replaced existing content:\n got: %q\nwant: %q", got, original)
	}
	info, statErr := os.Stat(filePath)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("existing mode=%o after failed create, want 600", info.Mode().Perm())
	}
}

func TestVerifyBlurbPresent(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("file with blurb", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "with-blurb.md")
		content := "# Test\n\n" + AgentBlurb
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		present, err := VerifyBlurbPresent(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if !present {
			t.Error("Expected blurb to be present")
		}
	})

	t.Run("file without blurb", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "without-blurb.md")
		if err := os.WriteFile(filePath, []byte("# Test\n\nNo blurb here"), 0644); err != nil {
			t.Fatal(err)
		}

		present, err := VerifyBlurbPresent(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if present {
			t.Error("Expected blurb to NOT be present")
		}
	})

	t.Run("malformed current blurb", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "malformed-blurb.md")
		content := "<!-- bv-agent-instructions-v4 -->\nmissing end marker"
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		present, err := VerifyBlurbPresent(filePath)
		if err == nil {
			t.Fatal("expected malformed marker error")
		}
		if present {
			t.Fatal("malformed marker must not verify as a present blurb")
		}
	})

	t.Run("duplicate current blurbs", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "duplicate-blurb.md")
		content := AgentBlurb + "\n\n" + AgentBlurb
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		present, err := VerifyBlurbPresent(filePath)
		if err == nil {
			t.Fatal("expected duplicate marker error")
		}
		if present {
			t.Fatal("duplicate blocks must not verify as one healthy blurb")
		}
	})

	t.Run("older blurb does not verify as current", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "old-blurb.md")
		content := "<!-- bv-agent-instructions-v3 -->\nold\n<!-- end-bv-agent-instructions -->"
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		present, err := VerifyBlurbPresent(filePath)
		if err == nil || present {
			t.Fatalf("VerifyBlurbPresent() present=%v err=%v, want false and version error", present, err)
		}
	})

	t.Run("fenced marker example does not verify", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "fenced-example.md")
		content := "```markdown\n<!-- bv-agent-instructions-v4 -->\nexample\n<!-- end-bv-agent-instructions -->\n```"
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		present, err := VerifyBlurbPresent(filePath)
		if err != nil || present {
			t.Fatalf("VerifyBlurbPresent() present=%v err=%v, want false, nil", present, err)
		}
	})

	t.Run("current and legacy blurbs", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "current-and-legacy.md")
		content := AgentBlurb + "\n\n" + LegacyBlurbContent
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		present, err := VerifyBlurbPresent(filePath)
		if err == nil {
			t.Fatal("expected remaining legacy blurb error")
		}
		if present {
			t.Fatal("versioned and legacy blocks must not verify as one healthy blurb")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := VerifyBlurbPresent(filepath.Join(tmpDir, "nonexistent.md"))
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
	})
}

func TestAtomicWritePreservesPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.md")

	// Create file with specific permissions
	if err := os.WriteFile(filePath, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}

	// Verify initial permissions
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("Initial permissions wrong: %o", info.Mode().Perm())
	}

	// Atomic write
	if err := atomicWrite(filePath, []byte("new content")); err != nil {
		t.Fatal(err)
	}

	// Verify permissions preserved
	info, err = os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("Permissions not preserved: expected 0600, got %o", info.Mode().Perm())
	}

	// Verify content changed
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new content" {
		t.Errorf("Content not updated: %s", content)
	}
}

func TestAtomicWriteNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "new-file.md")

	// Write to non-existent file
	if err := atomicWrite(filePath, []byte("brand new")); err != nil {
		t.Fatal(err)
	}

	// Verify file created
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "brand new" {
		t.Errorf("Unexpected content: %s", content)
	}
}

func TestEnsureBlurb(t *testing.T) {
	t.Run("no agent file - creates one", func(t *testing.T) {
		tmpDir := t.TempDir()

		if err := EnsureBlurb(tmpDir); err != nil {
			t.Fatal(err)
		}

		// Should have created AGENTS.md
		detection := DetectAgentFile(tmpDir)
		if !detection.Found() {
			t.Error("Expected AGENTS.md to be created")
		}
		if !detection.HasBlurb {
			t.Error("Expected blurb to be present")
		}
	})

	t.Run("agent file exists without blurb - appends", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "AGENTS.md")
		if err := os.WriteFile(filePath, []byte("# My Instructions\n\nExisting."), 0644); err != nil {
			t.Fatal(err)
		}

		if err := EnsureBlurb(tmpDir); err != nil {
			t.Fatal(err)
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "Existing.") {
			t.Error("Original content should be preserved")
		}
		if !strings.Contains(string(content), BlurbStartMarker) {
			t.Error("Blurb should be appended")
		}
	})

	t.Run("agent file with current blurb - no change", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "AGENTS.md")
		original := "# My Instructions\n\n" + AgentBlurb
		if err := os.WriteFile(filePath, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		if err := EnsureBlurb(tmpDir); err != nil {
			t.Fatal(err)
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		// Should not add duplicate
		count := strings.Count(string(content), BlurbStartMarker)
		if count != 1 {
			t.Errorf("Expected exactly 1 blurb, got %d", count)
		}
	})

	t.Run("malformed current blurb - errors without writing", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "AGENTS.md")
		original := "# My Instructions\n\n<!-- bv-agent-instructions-v4 -->\nunterminated user content"
		if err := os.WriteFile(filePath, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		if err := EnsureBlurb(tmpDir); err == nil {
			t.Fatal("expected malformed marker error")
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != original {
			t.Fatalf("EnsureBlurb changed malformed content:\n got: %q\nwant: %q", content, original)
		}
	})

	t.Run("duplicate current blurbs - consolidates", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "AGENTS.md")
		original := "# My Instructions\n\n" + AgentBlurb + "\n\nPreserve me.\n\n" + AgentBlurb
		if err := os.WriteFile(filePath, []byte(original), 0644); err != nil {
			t.Fatal(err)
		}

		if err := EnsureBlurb(tmpDir); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Count(string(content), BlurbStartMarker); got != 1 {
			t.Fatalf("current blurb count=%d, want 1", got)
		}
		if !strings.Contains(string(content), "Preserve me.") {
			t.Fatalf("duplicate consolidation lost user content:\n%s", content)
		}
	})
}

func TestAppendBlurbNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nonexistent.md")

	err := AppendBlurbToFile(filePath)
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestAtomicWriteNoPermission(t *testing.T) {
	// Skip on platforms where we can't test permissions properly
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test as root")
	}

	tmpDir := t.TempDir()

	// Create a read-only directory
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(readOnlyDir, 0755) // Cleanup

	filePath := filepath.Join(readOnlyDir, "test.md")

	// This should fail because we can't create temp file in read-only dir
	err := atomicWrite(filePath, []byte("test"))
	if err == nil {
		t.Error("Expected error writing to read-only directory")
	}
}
