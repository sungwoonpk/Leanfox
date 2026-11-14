package analyze

import (
	"path"
	"strings"
)

// subcommandTools are programs whose second word says what they do, so "git diff" and "git status" are told apart.
var subcommandTools = map[string]bool{
	"git": true, "go": true, "npm": true, "pnpm": true, "yarn": true, "cargo": true, "docker": true, "gh": true,
	"kubectl": true, "make": true, "pip": true, "pip3": true, "brew": true, "terraform": true, "bundle": true,
	"rustup": true, "poetry": true, "uv": true, "dotnet": true, "mvn": true, "gradle": true,
}

// wrappers run another program and say nothing about it.
var wrappers = map[string]bool{"sudo": true, "time": true, "env": true, "command": true, "nohup": true, "exec": true, "nice": true}

// setup statements prepare the shell and produce no output worth naming.
var setup = map[string]bool{"cd": true, "export": true, "set": true, "source": true, ".": true, "unset": true, "alias": true, "pushd": true, "popd": true}

// Family names the kind of a shell command by its program and, for tools with subcommands, the subcommand. It never
// includes arguments, because arguments are where secrets and private paths are.
func Family(command string) string {
	for _, stmt := range statements(command) {
		words := strings.Fields(stmt)
		words = skipLeading(words)
		if len(words) == 0 {
			continue
		}
		prog := strings.ToLower(path.Base(strings.Trim(words[0], `"'`)))
		if setup[prog] || prog == "" || prog == "/" {
			continue
		}
		if subcommandTools[prog] && len(words) > 1 && isSubcommand(words[1]) {
			return prog + " " + strings.ToLower(words[1])
		}
		return prog
	}
	return "shell"
}

// statements splits a script at newlines, "&&", "||" and ";". It does not understand quotes. A split inside a quoted
// string only makes a later piece unusable, and only the first usable piece is read.
func statements(s string) []string {
	r := strings.NewReplacer("&&", "\n", "||", "\n", ";", "\n", "|", "\n")
	parts := strings.Split(r.Replace(s), "\n")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		out = append(out, p)
	}
	return out
}

// skipLeading drops environment assignments and wrapper programs from the front of a command.
func skipLeading(words []string) []string {
	for len(words) > 0 {
		w := words[0]
		switch {
		case isAssignment(w):
			words = words[1:]
		case wrappers[w]:
			words = words[1:]
		case w == "timeout" && len(words) > 1:
			words = words[2:]
		default:
			return words
		}
	}
	return words
}

func isAssignment(w string) bool {
	i := strings.IndexByte(w, '=')
	if i <= 0 {
		return false
	}
	for j := 0; j < i; j++ {
		c := w[j]
		isLetter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
		if !(isLetter || (j > 0 && c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// isSubcommand accepts words like "diff", "run-script" and "pr", and rejects flags, paths and quoted text.
func isSubcommand(w string) bool {
	if w == "" || w[0] < 'a' || w[0] > 'z' {
		return false
	}
	for i := 0; i < len(w); i++ {
		c := w[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == ':') {
			return false
		}
	}
	return true
}
