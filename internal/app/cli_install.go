package app

import (
	"errors"
	"os"
	"path/filepath"
)

const cliCommandName = "perch"

// errCLIInstallCanceled reports that the user canceled an elevated install or
// uninstall prompt.
var errCLIInstallCanceled = errors.New("canceled by user")

// cliInstallResult describes a completed CLI installation.
type cliInstallResult struct {
	path   string
	inPath bool
}

// currentExecutablePath returns the resolved path of the running Perch binary.
func currentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return filepath.Clean(exe), nil
}

// dirInPATH reports whether dir appears in the current PATH environment
// variable.
func dirInPATH(dir string) bool {
	target := canonicalPath(dir)
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == "" {
			continue
		}
		if sameFolder(entry, target) {
			return true
		}
	}
	return false
}
