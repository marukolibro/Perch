// Package session stores the tabs and layout of an open project between
// launches.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Version is the format of the session files this Perch writes.
const Version int = 1

// keyBytes is how much of a project path's hash names its session file.
const keyBytes int = 16

// ErrVersion reports a session file of another version.
var ErrVersion = errors.New("session version is not supported")

// Tab is one open tab, and the state a text tab comes back to.
type Tab struct {
	Path   string  `json:"path"`
	Line   int     `json:"line"`
	Col    int     `json:"col"`
	Scroll float64 `json:"scroll"`
	// Source is set for a Markdown or SVG tab shown as source rather than
	// preview.
	Source bool `json:"source,omitempty"`
}

// RecentSearch is an executed project search and the options it used.
type RecentSearch struct {
	Query         string `json:"query"`
	CaseSensitive bool   `json:"caseSensitive"`
	Regexp        bool   `json:"regexp"`
	Include       string `json:"include,omitempty"`
	Exclude       string `json:"exclude,omitempty"`
}

// Session is the state of one project.
type Session struct {
	Version        int            `json:"version"`
	Tabs           []Tab          `json:"tabs"`
	Active         int            `json:"active"`
	SidebarWidth   float32        `json:"sidebarWidth"`
	SidebarTab     string         `json:"sidebarTab"`
	SidebarHidden  bool           `json:"sidebarHidden,omitempty"`
	OpenFolders    []string       `json:"openFolders,omitempty"`
	RecentSearches []RecentSearch `json:"recentSearches,omitempty"`
}

// Key names a project's session without its path in the file name.
func Key(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return hex.EncodeToString(sum[:keyBytes])
}

// Path returns the session file of root under dir.
func Path(dir string, root string) string {
	return filepath.Join(dir, Key(root)+".json")
}

// Load reads a session. A file that does not exist yet gives an empty session
// and no error. Another version reports ErrVersion.
func Load(file string) (Session, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Session{}, nil
	}
	if err != nil {
		return Session{}, fmt.Errorf("read session %q: %w", file, err)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return Session{}, fmt.Errorf("read session %q: %w", file, err)
	}
	if s.Version != Version {
		return Session{}, fmt.Errorf("read session %q: %w: version %d", file, ErrVersion, s.Version)
	}
	return s, nil
}

// Save writes a session through a temporary file, so that a failed write keeps
// the last session.
func Save(file string, s Session) error {
	s.Version = Version
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("save session %q: %w", file, err)
	}
	temp, err := os.CreateTemp(dir, ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("save session %q: %w", file, err)
	}
	_, writeErr := temp.Write(append(data, '\n'))
	if err := errors.Join(writeErr, temp.Close()); err != nil {
		return errors.Join(fmt.Errorf("save session %q: %w", file, err), os.Remove(temp.Name()))
	}
	if err := os.Rename(temp.Name(), file); err != nil {
		return errors.Join(fmt.Errorf("save session %q: %w", file, err), os.Remove(temp.Name()))
	}
	return nil
}
