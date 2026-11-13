package discover

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path string, age time.Duration, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := now.Add(-age)
	if err := os.Chtimes(path, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func names(files []File) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = filepath.Base(f.Path)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func tree(t *testing.T) (root string, now time.Time) {
	t.Helper()
	root = t.TempDir()
	now = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	write(t, filepath.Join(root, "-Users-me-app", "aaa111.jsonl"), 1*time.Hour, now)
	write(t, filepath.Join(root, "-Users-me-app", "bbb222.jsonl"), 48*time.Hour, now)
	write(t, filepath.Join(root, "-Users-me-site", "ccc333.jsonl"), 10*24*time.Hour, now)
	write(t, filepath.Join(root, "-Users-me-site", "notes.txt"), 1*time.Hour, now)
	write(t, filepath.Join(root, "-Users-me-site", "ccc333", "subagents", "agent-1.jsonl"), 1*time.Hour, now) // too deep
	return root, now
}

func TestFindsFilesOneFolderDownNewestFirst(t *testing.T) {
	root, now := tree(t)
	got, err := Find(Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"aaa111.jsonl", "bbb222.jsonl", "ccc333.jsonl"}; !equal(names(got), want) {
		t.Errorf("found %v, want %v", names(got), want)
	}
	if got[0].Project != "-Users-me-app" || got[0].Size != 3 {
		t.Errorf("first = %+v", got[0])
	}
}

func TestFilters(t *testing.T) {
	root, now := tree(t)
	cases := []struct {
		name string
		o    Options
		want []string
	}{
		{"since", Options{Since: 3 * 24 * time.Hour}, []string{"aaa111.jsonl", "bbb222.jsonl"}},
		{"project, ignoring case", Options{Project: "SITE"}, []string{"ccc333.jsonl"}},
		{"session prefix", Options{Session: "bbb"}, []string{"bbb222.jsonl"}},
		{"all three", Options{Since: 3 * 24 * time.Hour, Project: "app", Session: "aaa"}, []string{"aaa111.jsonl"}},
		{"nothing matches", Options{Project: "nope"}, []string{}},
	}
	for _, c := range cases {
		c.o.Root, c.o.Now = root, now
		got, err := Find(c.o)
		if err != nil {
			t.Fatal(err)
		}
		if !equal(names(got), c.want) {
			t.Errorf("%s: found %v, want %v", c.name, names(got), c.want)
		}
	}
}

func TestExplicitPaths(t *testing.T) {
	root, now := tree(t)
	file := filepath.Join(root, "-Users-me-app", "bbb222.jsonl")
	dir := filepath.Join(root, "-Users-me-site")
	got, err := Find(Options{Paths: []string{file, dir, file}, Now: now}) // the same file twice is listed once
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bbb222.jsonl", "ccc333.jsonl"}; !equal(names(got), want) {
		t.Errorf("found %v, want %v", names(got), want)
	}
	// A file that is not a session file is ignored even when named.
	txt := filepath.Join(dir, "notes.txt")
	got, err = Find(Options{Paths: []string{txt}, Now: now})
	if err != nil || len(got) != 0 {
		t.Errorf("found %v, err %v", got, err)
	}
}

func TestErrors(t *testing.T) {
	if _, err := Find(Options{}); err == nil {
		t.Error("no root and no paths must be an error")
	}
	if _, err := Find(Options{Root: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Error("a missing folder must be an error")
	}
	if _, err := Find(Options{Paths: []string{filepath.Join(t.TempDir(), "missing.jsonl")}}); err == nil {
		t.Error("a missing file must be an error")
	}
}

func TestLinksAreNotFollowed(t *testing.T) {
	root, now := tree(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.jsonl"), time.Hour, now)
	if err := os.Symlink(outside, filepath.Join(root, "-Users-me-link")); err != nil {
		t.Skip("links are not available here")
	}
	if err := os.Symlink(filepath.Join(outside, "secret.jsonl"), filepath.Join(root, "-Users-me-app", "linked.jsonl")); err != nil {
		t.Fatal(err)
	}
	got, err := Find(Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range got {
		if filepath.Base(f.Path) == "secret.jsonl" || filepath.Base(f.Path) == "linked.jsonl" {
			t.Errorf("followed a link to %s", f.Path)
		}
	}
	// A link that the user names, or a root that is itself a link, is followed: that is their choice.
	got, err = Find(Options{Paths: []string{filepath.Join(root, "-Users-me-link")}, Now: now})
	if err != nil || len(got) != 1 || filepath.Base(got[0].Path) != "secret.jsonl" {
		t.Errorf("a named link: found %v, err %v", got, err)
	}
}

func TestTheNumberOfFilesIsCapped(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	for i := range MaxFiles + 3 {
		path := filepath.Join(root, "p", "s"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+string(rune('a'+(i/676)%26))+".jsonl")
		write(t, path, time.Duration(i)*time.Second, now)
	}
	got, err := Find(Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != MaxFiles {
		t.Errorf("found %d files, want the cap of %d", len(got), MaxFiles)
	}
}

func TestDefaultRoot(t *testing.T) {
	none := func(string) string { return "" }
	if got := DefaultRoot(none, "/home/me"); got != filepath.Join("/home/me", ".claude", "projects") {
		t.Errorf("default root = %s", got)
	}
	cfg := func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return "/etc/claude"
		}
		return ""
	}
	if got := DefaultRoot(cfg, "/home/me"); got != filepath.Join("/etc/claude", "projects") {
		t.Errorf("configured root = %s", got)
	}
}

func TestAFolderThatCannotBeReadIsAnError(t *testing.T) {
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(locked, 0o700) }()
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	if _, err := Find(Options{Root: root}); err == nil {
		t.Error("an unreadable folder must be reported, not skipped silently")
	}
}

func TestFilesChangedAtTheSameTimeAreOrderedByPath(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	write(t, filepath.Join(root, "p", "b.jsonl"), time.Hour, now)
	write(t, filepath.Join(root, "p", "a.jsonl"), time.Hour, now)
	got, err := Find(Options{Root: root, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.jsonl", "b.jsonl"}; !equal(names(got), want) {
		t.Errorf("found %v, want %v", names(got), want)
	}
}
