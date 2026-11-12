//go:build !windows

package main

import (
	"errors"

	"sitecontrol/internal/config"
	"sitecontrol/internal/store"
)

// isWindowsService always reports false on non-Windows builds: there is no
// Windows Service Control Manager to have launched this process. Exists so
// main.go can call it unconditionally with no build tag of its own.
func isWindowsService() (bool, error) { return false, nil }

// runAsService is unreachable on non-Windows builds (isWindowsService always
// returns false above), but must exist with this signature so main.go
// compiles without a build tag.
func runAsService(cfg *config.Config, st *store.Store) error {
	return errors.New("sitectl: windows service mode is not supported on this platform")
}
