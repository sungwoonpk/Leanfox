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

`scripts/mutation-check.py` breaks the code in 40 or so ways, one line at a time, runs the tests after each, and lists the breakages
that no test noticed. It restores every file it touches. A survivor is either a missing test or a change that does not change
behavior, and each one that was found so far is described in [validation](validation.md).

```sh
python3 scripts/mutation-check.py
```

## Adding a model to the price table

Put the rates in `internal/pricing/pricing.go`. Mark the row verified only if a session file with a recorded cost for that model,
with every cost part in use, gives a `check:` line that says `exactly`. Otherwise leave it unverified and say why in a comment.
