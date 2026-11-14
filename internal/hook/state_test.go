package hook

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateDir(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for name, c := range map[string]struct {
		env  map[string]string
		want string
	}{
		"explicit":      {map[string]string{"LEANFOX_STATE_DIR": "/x", "CLAUDE_CONFIG_DIR": "/c"}, "/x"},
		"configuration": {map[string]string{"CLAUDE_CONFIG_DIR": "/c"}, filepath.Join("/c", "leanfox")},
		"default":       {map[string]string{}, filepath.Join("/home/me", ".claude", "leanfox")},
	} {
		if got := StateDir(get(c.env), "/home/me"); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

func TestModeIsOffUntilSetAndOffAgainWhenCleared(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "state")}
	if s.Mode() {
		t.Error("a new store must be off")
	}
	if err := s.SetMode(false); err != nil {
		t.Errorf("turning off what is off: %v", err)
	}
	if err := s.SetMode(true); err != nil || !s.Mode() {
		t.Fatalf("on: %v %v", err, s.Mode())
	}
	if err := s.SetMode(true); err != nil || !s.Mode() {
		t.Fatalf("on again: %v %v", err, s.Mode())
	}
	if err := s.SetMode(false); err != nil || s.Mode() {
		t.Fatalf("off: %v %v", err, s.Mode())
	}
	// Anything other than "on" in the file is off.
	if err := os.WriteFile(filepath.Join(s.Dir, "mode"), []byte("yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.Mode() {
		t.Error("a file that does not say on must be off")
	}
}

func TestStateIsPrivate(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "state")}
	if err := s.SetMode(true); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSession("abc", SessionState{Told: "on"}, now); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		s.Dir: 0o700, filepath.Join(s.Dir, "mode"): 0o600, filepath.Join(s.Dir, "sessions", "abc.json"): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s has mode %o, want %o", path, got, want)
		}
	}
	// No temporary file is left behind.
	for _, dir := range []string{s.Dir, filepath.Join(s.Dir, "sessions")} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if len(e.Name()) > 4 && e.Name()[:5] == ".tmp-" {
				t.Errorf("a temporary file was left: %s", e.Name())
			}
		}
	}
}

func TestSessionStateRoundTrips(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	want := SessionState{Told: "on", Crossed: []int64{150_000, 300_000}, GapNudged: 1234}
	if err := s.SaveSession("abc-123_x.y", want, now); err != nil {
		t.Fatal(err)
	}
	got := s.Session("abc-123_x.y")
	if got.Told != "on" || len(got.Crossed) != 2 || got.Crossed[1] != 300_000 || got.GapNudged != 1234 {
		t.Errorf("got %+v", got)
	}
	if s.Session("never-saved").Told != "" {
		t.Error("an unknown session must be new")
	}
}

func TestACorruptSessionFileIsTreatedAsNew(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(s.Dir, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "sessions", "abc.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.Session("abc"); got.Told != "" || got.Crossed != nil {
		t.Errorf("got %+v", got)
	}
}

func TestSessionIdsThatCouldNameAnotherPlaceAreRefused(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, id := range []string{"", ".", "..", "../x", "a/b", `a\b`, "a b", "a\x00b", "é", string(make([]byte, 129))} {
		if validID(id) {
			t.Errorf("%q must be refused", id)
		}
		if err := s.SaveSession(id, SessionState{Told: "on"}, now); err == nil {
			t.Errorf("saved a session called %q", id)
		}
		if got := s.Session(id); got.Told != "" {
			t.Errorf("read a session called %q", id)
		}
	}
	for _, id := range []string{"a", "A-b_c.d", "5b4a1c0e-0000-4000-8000-000000000001"} {
		if !validID(id) {
			t.Errorf("%q must be accepted", id)
		}
	}
}

func TestOldSessionStateIsForgotten(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, id := range []string{"old", "recent"} {
		if err := s.SaveSession(id, SessionState{Told: "on"}, now); err != nil {
			t.Fatal(err)
		}
	}
	old := now.Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(s.Dir, "sessions", "old.json"), old, old); err != nil {
		t.Fatal(err)
	}
	// Something that is not state is left alone.
	other := filepath.Join(s.Dir, "sessions", "notes.txt")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, old, old); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSession("trigger", SessionState{}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "sessions", "old.json")); err == nil {
		t.Error("a month-old session was kept")
	}
	for _, name := range []string{"recent.json", "trigger.json", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(s.Dir, "sessions", name)); err != nil {
			t.Errorf("%s was removed", name)
		}
	}
}

func TestWritingFailsCleanlyWhereAFolderCannotBeMade(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Store{Dir: filepath.Join(file, "inside")}
	if err := s.SetMode(true); err == nil {
		t.Error("expected an error")
	}
	if err := s.SaveSession("abc", SessionState{}, now); err == nil {
		t.Error("expected an error")
	}
}

func TestPruningAnEmptyOrMissingFolderIsFine(t *testing.T) {
	Store{Dir: filepath.Join(t.TempDir(), "nothing")}.prune(now)
}

func TestAModeThatCannotBeRemovedOrReplacedIsAnError(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	// A folder that is not empty sits where the file should be: it can be neither removed nor replaced.
	if err := os.MkdirAll(filepath.Join(s.Dir, "mode", "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMode(false); err == nil {
		t.Error("removing a folder that is not empty must be an error")
	}
	if err := s.SetMode(true); err == nil {
		t.Error("replacing a folder that is not empty must be an error")
	}
	entries, _ := os.ReadDir(s.Dir)
	for _, e := range entries {
		if e.Name() != "mode" {
			t.Errorf("a temporary file was left: %s", e.Name())
		}
	}
}
