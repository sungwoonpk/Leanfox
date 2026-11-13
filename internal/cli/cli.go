// Package cli is the command line of leanfox.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sungwoonpk/leanfox/internal/analyze"
	"github.com/sungwoonpk/leanfox/internal/discover"
	"github.com/sungwoonpk/leanfox/internal/pricing"
	"github.com/sungwoonpk/leanfox/internal/report"
	"github.com/sungwoonpk/leanfox/internal/transcript"
)

// Version is set at build time.
var Version = "dev"

// Exit codes.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Env is what the program needs from the machine, so that tests can supply their own.
type Env struct {
	Getenv func(string) string
	Home   string
	Now    func() time.Time
}

// OSEnv is the real environment.
func OSEnv() Env {
	home, _ := os.UserHomeDir()
	return Env{Getenv: os.Getenv, Home: home, Now: time.Now}
}

const usage = `leanfox measures where an AI coding agent's tokens and money go.

Usage:
  leanfox report [flags] [file or folder ...]   analyze Claude Code sessions
  leanfox version
  leanfox help

Run "leanfox report -h" for the flags of report.
`

// Run runs one command and returns the exit code.
func Run(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "report":
		return runReport(args[1:], stdout, stderr, env)
	case "version", "-v", "--version":
		fmt.Fprintf(stdout, "leanfox %s\n", Version)
		return ExitOK
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	}
	fmt.Fprintf(stderr, "leanfox: unknown command %q\n\n%s", args[0], usage)
	return ExitUsage
}

type reportFlags struct {
	json       bool
	since      string
	project    string
	session    string
	rates      string
	top        int
	handoff    int64
	perSession bool
}

func runReport(args []string, stdout, stderr io.Writer, env Env) int {
	var f reportFlags
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, `Usage: leanfox report [flags] [file or folder ...]

Reads Claude Code session files and says where the money went. With no file or folder it reads the
sessions of the last 30 days under ~/.claude/projects (or $CLAUDE_CONFIG_DIR/projects). Nothing leaves
the machine, and command arguments and file folders are never printed.

Flags:
`)
		fs.PrintDefaults()
	}
	fs.BoolVar(&f.json, "json", false, "write JSON instead of text")
	fs.StringVar(&f.since, "since", "", "only sessions changed within this long, such as 7d, 36h or 2w; 0 means no limit (default 30d, or no limit when files are named)")
	fs.StringVar(&f.project, "project", "", "only sessions whose project folder name contains this text")
	fs.StringVar(&f.session, "session", "", "only the session whose file name starts with this text")
	fs.StringVar(&f.rates, "rates", "", "a JSON file with prices for models that are not in the built-in table")
	fs.IntVar(&f.top, "top", 10, "how many single tool results to list")
	fs.Int64Var(&f.handoff, "handoff", 3000, "size in tokens of the note a restarted conversation would begin from")
	fs.BoolVar(&f.perSession, "per-session", false, "when there are several sessions, also write the full report of each")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}

	opts := analyze.Options{TopCarriers: f.top, HandoffTokens: f.handoff}
	if f.top < 1 || f.handoff < 0 {
		fmt.Fprintln(stderr, "leanfox: -top must be at least 1 and -handoff must not be negative")
		return ExitUsage
	}
	if f.rates != "" {
		over, err := readRates(f.rates)
		if err != nil {
			fmt.Fprintf(stderr, "leanfox: %v\n", err)
			return ExitError
		}
		opts.Table = pricing.NewTable(over)
	}

	paths := fs.Args()
	since, err := sinceDuration(f.since, len(paths) == 0)
	if err != nil {
		fmt.Fprintf(stderr, "leanfox: %v\n", err)
		return ExitUsage
	}
	files, err := discover.Find(discover.Options{
		Paths: paths, Root: discover.DefaultRoot(env.Getenv, env.Home), Since: since,
		Project: f.project, Session: f.session, Now: env.Now(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "leanfox: %v\n", err)
		return ExitError
	}
	if len(files) == 0 {
		fmt.Fprintln(stderr, "leanfox: no session files found. Name a file or folder, or widen -since.")
		return ExitError
	}

	var as []*analyze.Analysis
	empty := 0
	for _, file := range files {
		s, err := transcript.ParseFile(file.Path)
		if err != nil {
			fmt.Fprintf(stderr, "leanfox: skipping %s: %v\n", file.Path, err)
			continue
		}
		a := analyze.Analyze(s, opts)
		if a.Session.Requests == 0 {
			empty++
			continue
		}
		as = append(as, a)
	}
	if empty > 0 {
		fmt.Fprintf(stderr, "leanfox: %s had no requests and %s left out\n", plural(empty, "file"), map[bool]string{true: "was", false: "were"}[empty == 1])
	}
	if len(as) == 0 {
		fmt.Fprintln(stderr, "leanfox: nothing to report")
		return ExitError
	}

	if len(as) == 1 {
		if f.json {
			return writeJSON(stdout, stderr, as[0])
		}
		report.Session(stdout, as[0])
		return ExitOK
	}
	sum := analyze.Combine(as, f.top)
	if f.json {
		return writeJSON(stdout, stderr, struct {
			Sessions []*analyze.Analysis `json:"sessions"`
			Summary  *analyze.Summary    `json:"summary"`
		}{as, sum})
	}
	if f.perSession {
		for _, a := range as {
			report.Session(stdout, a)
			fmt.Fprintln(stdout, strings.Repeat("-", 60))
			fmt.Fprintln(stdout)
		}
	}
	report.Many(stdout, as, sum)
	return ExitOK
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	if err := report.JSON(stdout, v); err != nil {
		fmt.Fprintf(stderr, "leanfox: %v\n", err)
		return ExitError
	}
	return ExitOK
}

func readRates(path string) (map[string]pricing.Rates, error) {
	f, err := os.Open(path) // #nosec G304 -- the user named this file
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return pricing.ReadOverrides(f)
}

// sinceDuration reads -since. "7d" and "2w" are days and weeks; anything else is what time.ParseDuration reads. Empty
// means thirty days when looking in the default folder and no limit when files were named.
func sinceDuration(s string, defaultRoot bool) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "":
		if defaultRoot {
			return 30 * 24 * time.Hour, nil
		}
		return 0, nil
	case "0", "all":
		return 0, nil
	}
	unit := time.Duration(0)
	switch {
	case strings.HasSuffix(s, "d"):
		unit = 24 * time.Hour
	case strings.HasSuffix(s, "w"):
		unit = 7 * 24 * time.Hour
	}
	var d time.Duration
	if unit > 0 {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSuffix(s, "d"), "w"))
		if err != nil || n < 0 || n > 36500 {
			return 0, fmt.Errorf("-since %q is not a length of time such as 7d, 2w or 36h", s)
		}
		d = time.Duration(n) * unit
	} else {
		var err error
		d, err = time.ParseDuration(s)
		if err != nil || d < 0 {
			return 0, fmt.Errorf("-since %q is not a length of time such as 7d, 2w or 36h", s)
		}
	}
	return d, nil
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
