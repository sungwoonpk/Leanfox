// Package discover finds the session files that Claude Code keeps on this machine.
package discover

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MaxFiles bounds how many files one run will look at.
const MaxFiles = 5000

// File is one session file.
type File struct {
	Path    string
	Project string // the name of the folder it sits in, which is the working directory with dashes for slashes
	ModTime time.Time
	Size    int64
}

// Options say what to look for.
type Options struct {
	// Paths are files or folders. Empty means the default root.
	Paths []string
	// Root is the default root. Empty means the folder Claude Code uses: $CLAUDE_CONFIG_DIR/projects, or ~/.claude/projects.
	Root string
	// Since keeps files changed within this long. Zero keeps everything.
	Since time.Duration
	// Project keeps files whose folder name contains this text, ignoring case.
	Project string
	// Session keeps files whose name starts with this text.
	Session string
	// Now is the time that Since counts back from. Zero means the current time.
	Now time.Time
}

// DefaultRoot is where Claude Code writes its session files.
func DefaultRoot(getenv func(string) string, home string) string {
	if dir := getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects")
	}
	return filepath.Join(home, ".claude", "projects")
}

// Find lists session files, newest first. A folder is searched one level down as well, because the files sit in a folder
// per project. A path that was named, or the default root, may be a link: that is the user's choice, and many keep
// their configuration folder elsewhere. A link found inside it is not followed, and only regular files are taken, so
// what is in a folder cannot lead the search somewhere else.
func Find(o Options) ([]File, error) {
	roots := o.Paths
	if len(roots) == 0 {
		if o.Root == "" {
			return nil, errors.New("no folder to look in")
		}
		roots = []string{o.Root}
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}

	var out []File
	seen := map[string]bool{}
	add := func(path string, info fs.FileInfo) bool {
		if seen[path] || !strings.HasSuffix(path, ".jsonl") || !info.Mode().IsRegular() {
			return true
		}
		if o.Since > 0 && now.Sub(info.ModTime()) > o.Since {
			return true
		}
		project := filepath.Base(filepath.Dir(path))
		if o.Project != "" && !strings.Contains(strings.ToLower(project), strings.ToLower(o.Project)) {
			return true
		}
		if o.Session != "" && !strings.HasPrefix(filepath.Base(path), o.Session) {
			return true
		}
		seen[path] = true
		out = append(out, File{Path: path, Project: project, ModTime: info.ModTime(), Size: info.Size()})
		return len(out) < MaxFiles
	}

	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		switch {
		case info.Mode().IsRegular():
			add(root, info)
		case info.IsDir():
			if err := walk(root, 2, add); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%s is not a file or a folder", root)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// walk visits the files in dir and in the folders below it, down to the given depth. It stops when visit returns false.
func walk(dir string, depth int, visit func(string, fs.FileInfo) bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		info, err := os.Lstat(path)
		if err != nil {
			continue // it was removed while we looked
		}
		switch {
		case info.Mode().IsRegular():
			if !visit(path, info) {
				return nil
			}
		case info.IsDir() && depth > 1:
			if err := walk(path, depth-1, visit); err != nil {
				return err
			}
		}
	}
	return nil
}
