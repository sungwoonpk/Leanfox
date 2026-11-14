# Development

Needs Go 1.26 or newer, and for `make check` four tools that `go install` fetches:

```sh
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/kisielk/errcheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
```

| Command | What it does |
| --- | --- |
| `make test` | All tests |
| `make race` | All tests with the race detector, as CI runs them |
| `make cover` | Coverage of the libraries |
| `make lint` | gofmt, vet, staticcheck, errcheck, gosec |
| `make vuln` | govulncheck |
| `make check` | All of the above, in the order CI runs them |
| `make demo` | Build, then report on the made-up example session |

## Layout

- `internal/transcript` reads a session file into events. It is the only code that knows the file format.
- `internal/pricing` turns token counts into dollars.
- `internal/analyze` measures a session: cost, context, the fit, carriers, repeats, the what-ifs, and `Combine` for several.
- `internal/report` writes an analysis as text or JSON.
- `internal/discover` finds session files.
- `internal/hook` is lean mode: the command parser, the state files, the rules, and the notices. It is what `leanfox hook` runs.
- `.claude-plugin/`, `hooks/` and `skills/` are the Claude Code plugin. They are only files, and `cmd/leanfox/plugin_test.go`
  and `main_test.go` check that they parse and that the hook commands in `hooks/hooks.json` work.
- `internal/cli` is the command line. `cmd/leanfox` only calls it.

## The example session

`examples/demo-session.jsonl` is made by `exampleSession` in `internal/cli/example_test.go`. After changing the generator, or
anything that changes the report, run:

```sh
go test ./internal/cli -run TestExample -update
make demo
```

and copy the start of the output into the README between the `sample` markers. Two tests fail until both are done.

## Mutation checking

`scripts/mutation-check.py` breaks the code in about 60 ways, one line at a time, runs the tests after each, and lists the breakages
that no test noticed. It restores every file it touches. A survivor is either a missing test or a change that does not change
behavior, and each one that was found so far is described in [validation](validation.md).

```sh
python3 scripts/mutation-check.py
```

## Trying lean mode in a real Claude Code

Unit tests cannot say that Claude Code accepts the plugin. This does, for a few cents, without touching your real settings:

```sh
S=$(mktemp -d); mkdir "$S/bin" "$S/state"; go build -o "$S/bin/leanfox" ./cmd/leanfox
PATH="$S/bin:$PATH" LEANFOX_STATE_DIR="$S/state" claude -p "/leanfox" --plugin-dir "$PWD" --model haiku --no-session-persistence
cat "$S/state/mode"      # on
```

`LEANFOX_STATE_DIR` keeps the state out of `~/.claude`. Run `/leanfox` again to see it turn off, and ask a new session whether its
context contains "LEANFOX MODE IS ON" to see the rules.

## Adding a model to the price table

Put the rates in `internal/pricing/pricing.go`. Mark the row verified only if a session file with a recorded cost for that model,
with every cost part in use, gives a `check:` line that says `exactly`. Otherwise leave it unverified and say why in a comment.
