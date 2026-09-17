package main

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// loadBoardHideEmptyColumnsPreference reads the interactive board preference
// from the user's saved config. Invalid, unavailable, or disabled saved config
// keeps the board's existing Auto behavior.
func loadBoardHideEmptyColumnsPreference() bool {
	if os.Getenv("BV_NO_SAVED_CONFIG") != "" {
		return false
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(homeDir, ".config", "bv", "config.yaml"))
	if err != nil {
		return false
	}

	var cfg struct {
		Board struct {
			HideEmptyColumns bool `yaml:"hide_empty_columns"`
		} `yaml:"board"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return false
	}
	return cfg.Board.HideEmptyColumns
}
