package main

import (
	"os"
	"path/filepath"
	"testing"
)

func withBoardConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "bv")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", configDir, err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("BV_NO_SAVED_CONFIG", "")
}

func TestLoadBoardHideEmptyColumnsPreference(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "enabled", body: "board:\n  hide_empty_columns: true\n", want: true},
		{name: "disabled", body: "board:\n  hide_empty_columns: false\n"},
		{name: "missing key", body: "theme: dark\n"},
		{name: "malformed yaml", body: "board: [unterminated\n"},
		{name: "wrong type", body: "board:\n  hide_empty_columns: not-a-bool\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withBoardConfig(t, tt.body)
			if got := loadBoardHideEmptyColumnsPreference(); got != tt.want {
				t.Fatalf("loadBoardHideEmptyColumnsPreference() = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("missing file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("BV_NO_SAVED_CONFIG", "")
		if got := loadBoardHideEmptyColumnsPreference(); got {
			t.Fatal("missing config should retain Auto behavior")
		}
	})

	t.Run("unreadable file", func(t *testing.T) {
		home := t.TempDir()
		configDir := filepath.Join(home, ".config", "bv")
		if err := os.MkdirAll(filepath.Join(configDir, "config.yaml"), 0o755); err != nil {
			t.Fatalf("mkdir config.yaml: %v", err)
		}
		t.Setenv("HOME", home)
		t.Setenv("BV_NO_SAVED_CONFIG", "")
		if got := loadBoardHideEmptyColumnsPreference(); got {
			t.Fatal("unreadable config should retain Auto behavior")
		}
	})

	t.Run("saved config disabled", func(t *testing.T) {
		withBoardConfig(t, "board:\n  hide_empty_columns: true\n")
		t.Setenv("BV_NO_SAVED_CONFIG", "1")
		if got := loadBoardHideEmptyColumnsPreference(); got {
			t.Fatal("BV_NO_SAVED_CONFIG should retain Auto behavior")
		}
	})
}
