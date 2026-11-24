//go:build !windows

package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const cliScriptMarker = "# Installed by Perch."

// cliScript builds the POSIX sh launcher that invokes executable in the
// background while preserving the terminal's working directory.
func cliScript(executable string) string {
	quoted := posixSingleQuote(executable)
	return "#!/bin/sh\n" +
		cliScriptMarker + "\n" +
		"BIN=" + quoted + "\n" +
		"if [ ! -x \"$BIN\" ]; then\n" +
		"  printf 'perch: Perch executable not found at %s\\n' \"$BIN\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"case \"${1:-}\" in\n" +
		"  -h|--help|-v|--version)\n" +
		"    exec \"$BIN\" \"$@\"\n" +
		"    ;;\n" +
		"esac\n" +
		"nohup \"$BIN\" \"$@\" >/dev/null 2>&1 </dev/null &\n"
}

func posixSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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

// cliCandidatePaths lists the standard locations where Perch may install its
// CLI launcher, in lookup order.
func cliCandidatePaths() []string {
	var paths []string
	if runtime.GOOS == "darwin" {
		paths = append(paths,
			filepath.Join("/usr/local/bin", cliCommandName),
			filepath.Join("/opt/homebrew/bin", cliCommandName),
		)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".local", "bin", cliCommandName))
	}
	return paths
}

// cliInstalledPath returns the path of an installed Perch-managed CLI script
// that matches the current executable, or empty when not installed (or pointing
// to a different location).
func cliInstalledPath() string {
	exe, err := currentExecutablePath()
	if err != nil {
		return ""
	}
	wantLine := "BIN=" + posixSingleQuote(exe)
	for _, path := range cliCandidatePaths() {
		content, managed := isPerchManagedScript(path)
		if managed && strings.Contains(content, wantLine) {
			return path
		}
	}
	return ""
}

// cliTargetPath chooses where to install the CLI launcher.
func cliTargetPath() (string, error) {
	// Update any existing Perch-managed launcher in place.
	for _, path := range cliCandidatePaths() {
		if _, managed := isPerchManagedScript(path); managed {
			return path, nil
		}
	}
	if runtime.GOOS == "darwin" {
		for _, dir := range []string{"/usr/local/bin", "/opt/homebrew/bin"} {
			if dirWritable(dir) {
				return filepath.Join(dir, cliCommandName), nil
			}
		}
		return filepath.Join("/usr/local/bin", cliCommandName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".local", "bin", cliCommandName), nil
}

func dirWritable(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	tmp, err := os.CreateTemp(dir, ".perch-probe-*")
	if err != nil {
		return false
	}
	name := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(name)
	return true
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
	if err := installCLITo(target, exe); err != nil {
		return cliInstallResult{}, err
	}
	return cliInstallResult{
		path:   target,
		inPath: dirInPATH(filepath.Dir(target)),
	}, nil
}

func installCLITo(target string, executable string) error {
	if _, err := os.Lstat(target); err == nil {
		if _, managed := isPerchManagedScript(target); !managed {
			return fmt.Errorf("%s already exists and was not installed by Perch", target)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	script := cliScript(executable)
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		if runtime.GOOS == "darwin" && errors.Is(err, fs.ErrPermission) {
			return installCLIDarwinElevated(target, script)
		}
		return err
	}
	if err := writeExecutableFile(target, []byte(script)); err != nil {
		if runtime.GOOS == "darwin" && errors.Is(err, fs.ErrPermission) {
			return installCLIDarwinElevated(target, script)
		}
		return err
	}
	return nil
}

func writeExecutableFile(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".perch-cli-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func uninstallCLI() (string, error) {
	target := cliInstalledPath()
	if target == "" {
		// Also check for any stale Perch-managed script.
		for _, path := range cliCandidatePaths() {
			if _, managed := isPerchManagedScript(path); managed {
				target = path
				break
			}
		}
	}
	if target == "" {
		return "", nil
	}
	if err := uninstallCLIFrom(target); err != nil {
		return "", err
	}
	return target, nil
}

func uninstallCLIFrom(target string) error {
	if _, managed := isPerchManagedScript(target); !managed {
		return fmt.Errorf("%s was not installed by Perch", target)
	}
	if err := os.Remove(target); err != nil {
		if runtime.GOOS == "darwin" && errors.Is(err, fs.ErrPermission) {
			return removeDarwinElevated(target)
		}
		return err
	}
	return nil
}

func installCLIDarwinElevated(target string, script string) error {
	tmp, err := os.CreateTemp("", "perch-cli-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.WriteString(script); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	shellCmd := fmt.Sprintf(
		"install -d -m 755 %s && install -m 755 %s %s",
		posixSingleQuote(filepath.Dir(target)),
		posixSingleQuote(tmpPath),
		posixSingleQuote(target),
	)
	return runDarwinElevated(shellCmd)
}

func removeDarwinElevated(target string) error {
	shellCmd := fmt.Sprintf("rm -f %s", posixSingleQuote(target))
	return runDarwinElevated(shellCmd)
}

func runDarwinElevated(shellCmd string) error {
	appleScript := fmt.Sprintf("do shell script %q with administrator privileges", shellCmd)
	output, err := exec.Command("osascript", "-e", appleScript).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if strings.Contains(text, "User canceled") || strings.Contains(text, "(-128)") {
			return errCLIInstallCanceled
		}
		if text != "" {
			return errors.New(text)
		}
		return err
	}
	return nil
}
