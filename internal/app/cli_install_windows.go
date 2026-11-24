//go:build windows

package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const cliScriptMarker = ":: Installed by Perch."

func cliScript(executable string) string {
	return "@echo off\r\n" +
		cliScriptMarker + "\r\n" +
		"set \"BIN=" + executable + "\"\r\n" +
		"if \"%~1\"==\"-h\" (\"%BIN%\" %* & exit /b %errorlevel%)\r\n" +
		"if \"%~1\"==\"--help\" (\"%BIN%\" %* & exit /b %errorlevel%)\r\n" +
		"if \"%~1\"==\"-v\" (\"%BIN%\" %* & exit /b %errorlevel%)\r\n" +
		"if \"%~1\"==\"--version\" (\"%BIN%\" %* & exit /b %errorlevel%)\r\n" +
		"start \"\" \"%BIN%\" %*\r\n"
}

func cliTargetPath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		base = dir
	}
	return filepath.Join(base, "Perch", "bin", cliCommandName+".cmd"), nil
}

func isPerchManagedScript(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := string(data)
	if !strings.Contains(text, cliScriptMarker) {
		return "", false
	}
	return text, true
}

func cliInstalledPath() string {
	exe, err := currentExecutablePath()
	if err != nil {
		return ""
	}
	target, err := cliTargetPath()
	if err != nil {
		return ""
	}
	content, managed := isPerchManagedScript(target)
	if managed && strings.Contains(content, "set \"BIN="+exe+"\"") {
		return target
	}
	return ""
}

func installCLI() (cliInstallResult, error) {
	exe, err := currentExecutablePath()
	if err != nil {
		return cliInstallResult{}, err
	}
	target, err := cliTargetPath()
	if err != nil {
		return cliInstallResult{}, err
	}
	if _, err := os.Lstat(target); err == nil {
		if _, managed := isPerchManagedScript(target); !managed {
			return cliInstallResult{}, fmt.Errorf("%s already exists and was not installed by Perch", target)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return cliInstallResult{}, err
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return cliInstallResult{}, err
	}
	if err := os.WriteFile(target, []byte(cliScript(exe)), 0o644); err != nil {
		return cliInstallResult{}, err
	}
	inPath := addWindowsUserPath(dir) == nil
	return cliInstallResult{path: target, inPath: inPath}, nil
}

func uninstallCLI() (string, error) {
	target, err := cliTargetPath()
	if err != nil {
		return "", err
	}
	if _, managed := isPerchManagedScript(target); !managed {
		return "", nil
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	_ = removeWindowsUserPath(filepath.Dir(target))
	return target, nil
}

func addWindowsUserPath(dir string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = key.Close() }()
	current, _, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	for _, entry := range filepath.SplitList(current) {
		if strings.EqualFold(filepath.Clean(entry), filepath.Clean(dir)) {
			return nil
		}
	}
	next := dir
	if current != "" {
		next = strings.TrimRight(current, ";") + ";" + dir
	}
	return key.SetExpandStringValue("Path", next)
}

func removeWindowsUserPath(dir string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = key.Close() }()
	current, _, err := key.GetStringValue("Path")
	if err != nil {
		return err
	}
	var kept []string
	for _, entry := range filepath.SplitList(current) {
		if entry == "" || strings.EqualFold(filepath.Clean(entry), filepath.Clean(dir)) {
			continue
		}
		kept = append(kept, entry)
	}
	return key.SetExpandStringValue("Path", strings.Join(kept, ";"))
}
