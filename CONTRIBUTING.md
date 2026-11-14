# Contributing

## Set up

Needs Go 1.26 or newer. There is nothing else to install. Use a recent patch release: the standard library is part of what
ships, and `make vuln` fails on the flaws that older patches have.

```sh
git clone https://github.com/sungwoonpk/Leanfox.git
cd Leanfox
make check      # format, vet, staticcheck, errcheck, gosec, race tests with coverage, vulnerability scan
```

`make check` needs four tools. [docs/development.md](docs/development.md) lists them, the layout of the code, how the example
session is regenerated, and how to run the mutation check.

## What a change needs

- A test for the behavior, and for a bug fix a test that failed before the fix.
- A change to what the report says needs the example regenerated and the README sample updated (two tests fail until it is).
- A number in the documents comes from a command that anyone can run, or says where it comes from.
- A new model in the price table is marked verified only under the condition in [docs/development.md](docs/development.md).
- No real session file, and nothing derived from one beyond sizes and counts, in a test, an example or a document.
