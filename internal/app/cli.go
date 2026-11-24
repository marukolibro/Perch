package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui/editor"
)

// launchTarget is one folder or file location requested on the command line.
type launchTarget struct {
	// path is the resolved absolute filesystem path.
	path string
	// isDir reports whether path is an existing directory.
	isDir bool
	// line and col are 1-based; 0 means unspecified.
	line int
	col  int
	// err reports why the argument could not be opened.
	err error
}

// HandleCLIInfo answers --help / -h and --version / -v on stdout and reports
// whether the process should exit without launching the UI.
func HandleCLIInfo(args []string, version string, out io.Writer) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "-h", "--help":
		_, _ = fmt.Fprint(out,
			"Usage: perch [path | file:line[:col]]...\n\n"+
				"Open folders or files in Perch, optionally jumping to a 1-based line and column.\n\n"+
				"Options:\n"+
				"  -g, --goto       Ignored (accepted for compatibility with code -g)\n"+
				"  -h, --help       Show this help message\n"+
				"  -v, --version    Show Perch version\n",
		)
		return true
	case "-v", "--version":
		if version == "" {
			version = "dev"
		}
		_, _ = fmt.Fprintf(out, "Perch %s\n", version)
		return true
	default:
		return false
	}
}

// parseLaunchArgs turns command-line arguments after the program name into
// resolved folders and file locations relative to workingDir.
func parseLaunchArgs(args []string, workingDir string) []launchTarget {
	cwd := workingDir
	var targets []launchTarget
	literal := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if !literal {
			switch {
			case arg == "--":
				literal = true
				continue
			case strings.HasPrefix(arg, "-psn_"):
				// macOS LaunchServices passes a process serial number when
				// launching an app bundle from Finder.
				continue
			case arg == "-g", arg == "--goto":
				continue
			case arg == "--cwd":
				if i+1 < len(args) {
					i++
					cwd = args[i]
				}
				continue
			case strings.HasPrefix(arg, "--cwd="):
				cwd = strings.TrimPrefix(arg, "--cwd=")
				continue
			}
		}
		targets = append(targets, resolveLaunchTarget(arg, cwd))
	}
	return targets
}

// resolveLaunchTarget resolves a single CLI argument, preferring an exact
// existing path before interpreting trailing :line or :line:col suffixes.
func resolveLaunchTarget(raw string, workingDir string) launchTarget {
	absRaw, err := resolvePath(raw, workingDir)
	if err != nil {
		return launchTarget{path: raw, err: err}
	}
	info, rawErr := os.Stat(absRaw)
	if rawErr == nil {
		return launchTarget{path: absRaw, isDir: info.IsDir()}
	}

	// Try :line:col first, then :line, so a file literally named "a:10" still
	// opens with "a:10:20" when "a" does not exist.
	for _, candidate := range lineColCandidates(raw) {
		absPath, absErr := resolvePath(candidate.path, workingDir)
		if absErr != nil {
			continue
		}
		stat, statErr := os.Stat(absPath)
		if statErr == nil && !stat.IsDir() {
			return launchTarget{
				path: absPath,
				line: candidate.line,
				col:  candidate.col,
			}
		}
	}

	// When the raw path looked like file:line[:col] and did not exist, report
	// the stripped file path so the error names the missing file cleanly.
	if candidates := lineColCandidates(raw); len(candidates) > 0 {
		best := candidates[0]
		if absPath, absErr := resolvePath(best.path, workingDir); absErr == nil {
			if _, statErr := os.Stat(absPath); statErr != nil {
				return launchTarget{path: absPath, line: best.line, col: best.col, err: statErr}
			}
		}
	}

	return launchTarget{path: absRaw, err: rawErr}
}

type pathLineCol struct {
	path string
	line int
	col  int
}

// lineColCandidates returns possible (path, line, col) interpretations of raw
// from most specific (:line:col) to least (:line).
func lineColCandidates(raw string) []pathLineCol {
	head, tail, ok := cutLastColon(raw)
	if !ok {
		return nil
	}
	lastNum, ok := parsePositiveInt(tail)
	if !ok {
		return nil
	}
	var out []pathLineCol
	if head2, tail2, ok2 := cutLastColon(head); ok2 {
		if lineNum, okNum := parsePositiveInt(tail2); okNum {
			out = append(out, pathLineCol{path: head2, line: lineNum, col: lastNum})
		}
	}
	out = append(out, pathLineCol{path: head, line: lastNum})
	return out
}

// cutLastColon splits text at its last colon unless the prefix is empty or a
// Windows drive letter (such as "C:").
func cutLastColon(text string) (string, string, bool) {
	index := strings.LastIndexByte(text, ':')
	if index <= 0 {
		return "", "", false
	}
	head := text[:index]
	if len(head) == 1 && isASCIIAlpha(head[0]) {
		return "", "", false
	}
	return head, text[index+1:], true
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func parsePositiveInt(text string) (int, bool) {
	if text == "" {
		return 0, false
	}
	for i := range len(text) {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

func resolvePath(path string, workingDir string) (string, error) {
	if !filepath.IsAbs(path) && workingDir != "" {
		path = filepath.Join(workingDir, path)
	}
	return filepath.Abs(path)
}

// projectRootForFile returns the nearest ancestor directory containing a .git
// entry (directory for a standard repository, file for a worktree or
// submodule), or the file's parent directory when it is outside Git.
func projectRootForFile(file string) string {
	dir := filepath.Dir(file)
	for current := dir; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return dir
		}
	}
}

// canonicalPath resolves symlinks when possible so paths like /var and
// /private/var compare equal.
func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	if realPath, err := filepath.EvalSymlinks(abs); err == nil {
		return realPath
	}
	return abs
}

// sameFolder reports whether two project roots refer to the same directory.
func sameFolder(a string, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return canonicalPath(a) == canonicalPath(b)
}

// windowForFile finds the open project window containing file. When nested
// folders are open at once, the deepest matching root wins.
func windowForFile(windows []*App, file string) (*App, string) {
	var best *App
	var bestRel string
	bestDepth := -1
	for _, a := range windows {
		if a.root == "" {
			continue
		}
		rel, ok := insideProject(a.root, file)
		if !ok {
			continue
		}
		depth := len(canonicalPath(a.root))
		if depth > bestDepth {
			best = a
			bestRel = rel
			bestDepth = depth
		}
	}
	return best, bestRel
}

// positionForLineCol converts 1-based line and optional 1-based column (in
// runes) into a clamped 0-based editor position whose Col is a valid UTF-8 byte
// offset within the line.
func positionForLineCol(buffer *editor.Buffer, line int, col int) editor.Pos {
	count := buffer.LineCount()
	if count <= 0 {
		return editor.Pos{}
	}
	lineIdx := min(max(line-1, 0), count-1)
	if col <= 1 {
		return editor.Pos{Line: lineIdx}
	}
	text := buffer.Line(lineIdx)
	byteCol := 0
	runes := 1
	for byteCol < len(text) && runes < col {
		_, size := utf8.DecodeRuneInString(text[byteCol:])
		byteCol += size
		runes++
	}
	return editor.Pos{Line: lineIdx, Col: byteCol}
}
