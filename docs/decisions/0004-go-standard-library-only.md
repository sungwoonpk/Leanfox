# 0004. Go, with the standard library only

Status: accepted

## Context

The tool reads files of tens of megabytes, and it may later run as a hook on every command an agent makes, where start-up time
is paid each time.

## Decision

Go, with no dependency at all. A session of 45 MB is read and analyzed in well under a second. The binary is one static file for
Linux, macOS and Windows, and builds without a C compiler. It links no networking package, and a test checks that.

## Costs

- The least-squares solver, the JSON handling and the report writing are written here rather than imported. The solver is
  checked against an exhaustive search.
- No dependency also means no dependency to be audited, which is part of why a tool that reads private files can claim it
  does not send them anywhere.

## Alternatives considered

- **TypeScript, as the author's other tools are.** Node's start-up time, typically tens of milliseconds, is paid on every call if this
  becomes a hook, and there would be a runtime to install. A single binary is easier to trust and to run.
- **A numeric library for the fit.** It would save a hundred lines, and it would be the only thing in the program with a
  dependency, for a problem with six columns.
