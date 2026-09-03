package ui

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Prevent any test from accidentally opening a browser
	os.Setenv("BV_NO_BROWSER", "1")
	os.Setenv("BV_TEST_MODE", "1")

	// Keep every test away from the developer's real ~/.config/bv: saved
	// config (tutorial progress, update-check disclosure) is disabled unless a
	// test opts back in with t.Setenv, and the XDG config dir points at a
	// throwaway directory so even opted-in tests never touch the real one.
	os.Setenv("BV_NO_SAVED_CONFIG", "1")
	isolatedConfig, err := os.MkdirTemp("", "bv-ui-test-config-*")
	if err != nil {
		panic("creating isolated XDG_CONFIG_HOME: " + err.Error())
	}
	os.Setenv("XDG_CONFIG_HOME", isolatedConfig)

	code := m.Run()
	os.RemoveAll(isolatedConfig)
	os.Exit(code)
}
