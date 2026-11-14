package hook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store keeps what Leanfox remembers between prompts: whether lean mode is on, and for each session what its model was last
// told. It is a folder of small files, so that two sessions open at once cannot corrupt each other's state.
type Store struct{ Dir string }

// StateDir is where the state lives: $LEANFOX_STATE_DIR if set, else a folder inside Claude Code's own configuration
// folder, so that it follows the same account and is removed with it.
func StateDir(getenv func(string) string, home string) string {
	if d := getenv("LEANFOX_STATE_DIR"); d != "" {
		return d
	}
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "leanfox")
	}
	return filepath.Join(home, ".claude", "leanfox")
}

func (s Store) modePath() string { return filepath.Join(s.Dir, "mode") }

// Mode reports whether lean mode is on. A missing or unreadable file is off.
func (s Store) Mode() bool {
	b, err := os.ReadFile(s.modePath()) // #nosec G304 -- inside the state folder
	return err == nil && strings.TrimSpace(string(b)) == "on"
}

// SetMode turns lean mode on or off.
func (s Store) SetMode(on bool) error {
	if !on {
		if err := os.Remove(s.modePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return writeAtomic(s.modePath(), []byte("on\n"))
}

// SessionState is what Leanfox remembers about one session.
type SessionState struct {
	// Told is what the model in this session was last told about lean mode: "on", "off", or empty if nothing yet.
	Told string `json:"told"`
	// Crossed are the context sizes, in tokens, that have already been mentioned.
	Crossed []int64 `json:"crossed,omitempty"`
	// GapNudged is the time, as seconds since 1970, of the last response that a pause was mentioned for.
	GapNudged int64 `json:"gap_nudged,omitempty"`
}

const (
	maxSessionFiles = 2000
	sessionKeepFor  = 30 * 24 * time.Hour
)

// validID accepts what Claude Code uses for a session id and nothing that could name another folder.
func validID(id string) bool {
	if id == "" || len(id) > 128 || id == "." || id == ".." {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

func (s Store) sessionPath(id string) string { return filepath.Join(s.Dir, "sessions", id+".json") }

// Session returns what is remembered about a session. A session with no state, or with state that cannot be read, is
// treated as new.
func (s Store) Session(id string) SessionState {
	var st SessionState
	if !validID(id) {
		return st
	}
	b, err := os.ReadFile(s.sessionPath(id)) // #nosec G304 -- the id was validated
	if err != nil || json.Unmarshal(b, &st) != nil {
		return SessionState{}
	}
	return st
}

// SaveSession remembers the state of a session, and forgets sessions that have not been touched for a month.
func (s Store) SaveSession(id string, st SessionState, now time.Time) error {
	if !validID(id) {
		return errors.New("invalid session id")
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if err := writeAtomic(s.sessionPath(id), b); err != nil {
		return err
	}
	s.prune(now)
	return nil
}

func (s Store) prune(now time.Time) {
	dir := filepath.Join(s.Dir, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for i, e := range entries {
		if i >= maxSessionFiles {
			return
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if now.Sub(info.ModTime()) > sessionKeepFor {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// writeAtomic writes a file so that a reader sees the old content or the new one and never half of it.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		return errors.Join(werr, cerr)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
