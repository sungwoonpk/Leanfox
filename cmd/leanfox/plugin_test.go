package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The plugin is only files, and a mistake in one of them shows up as a plugin that silently does nothing. These tests
// check what can be checked without Claude Code: that the files parse and say what the program expects.

func read(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPluginManifest(t *testing.T) {
	var m struct {
		Name, Version, Description string
		Author                     struct{ Name string }
	}
	if err := json.Unmarshal(read(t, ".claude-plugin/plugin.json"), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "leanfox" || m.Version == "" || m.Description == "" || m.Author.Name == "" {
		t.Errorf("manifest = %+v", m)
	}
	if strings.ContainsAny(m.Name, " @:") || m.Name != strings.ToLower(m.Name) {
		t.Errorf("the name %q must be lower case without spaces, @ or :", m.Name)
	}
}

func TestMarketplaceListsThePlugin(t *testing.T) {
	var m struct {
		Name    string
		Owner   struct{ Name string }
		Plugins []struct{ Name, Source, Description string }
	}
	if err := json.Unmarshal(read(t, ".claude-plugin/marketplace.json"), &m); err != nil {
		t.Fatal(err)
	}
	if m.Name == "" || m.Owner.Name == "" || len(m.Plugins) != 1 {
		t.Fatalf("marketplace = %+v", m)
	}
	if p := m.Plugins[0]; p.Name != "leanfox" || p.Source != "./" || p.Description == "" {
		t.Errorf("plugin entry = %+v", p)
	}
}

func TestHooksCallTheProgramAndNeverFailWithoutIt(t *testing.T) {
	var h struct {
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct {
				Type, Command string
				Timeout       int
			}
		}
	}
	if err := json.Unmarshal(read(t, "hooks/hooks.json"), &h); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"SessionStart": "session-start", "UserPromptSubmit": "user-prompt"}
	if len(h.Hooks) != len(want) {
		t.Errorf("events = %v, want %v", h.Hooks, want)
	}
	for event, arg := range want {
		groups := h.Hooks[event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s: %+v", event, groups)
		}
		hk := groups[0].Hooks[0]
		if hk.Type != "command" || hk.Timeout <= 0 || hk.Timeout > 30 {
			t.Errorf("%s: %+v", event, hk)
		}
		// Without the program the hook must exit 0 and say nothing, and with it the hook must call the right event.
		if !strings.HasPrefix(hk.Command, "command -v leanfox >/dev/null 2>&1 || exit 0;") || !strings.HasSuffix(hk.Command, "leanfox hook "+arg) {
			t.Errorf("%s: command %q", event, hk.Command)
		}
	}
}

func TestSkillIsNamedLikeTheCommand(t *testing.T) {
	text := string(read(t, "skills/leanfox/SKILL.md"))
	if !strings.HasPrefix(text, "---\nname: leanfox\ndescription: ") {
		t.Errorf("the front matter is wrong:\n%.200s", text)
	}
	if end := strings.Index(text[4:], "\n---\n"); end < 0 {
		t.Error("the front matter is not closed")
	}
	for _, want := range []string{"go install github.com/sungwoonpk/leanfox/cmd/leanfox@latest", "Never use this skill on your own"} {
		if !strings.Contains(text, want) {
			t.Errorf("the skill lacks %q", want)
		}
	}
}
