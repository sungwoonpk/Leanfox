# 0001. Measure where the tokens go before reducing any of them

Status: accepted

## Context

The project began as a way to spend fewer tokens on a coding agent without making the agent's work worse. The obvious first
tool is a filter on command output: shorten what `git log`, a test run or a build prints before the agent sees it. Such tools
exist. Whether one pays depends on how much of the cost command output is, and nobody who installs one is told.

The session files that Claude Code writes hold enough to find out: the usage of every request, the size of every piece of text,
and the cost that Claude Code recorded.

## Decision

The first thing built is the measurement, and nothing that changes what an agent sees is built until the measurement says it
would pay.

## What the measurement said

On two long sessions, tool results were 13% and 24% of the money spent on input. What the model wrote into its own tool calls
was 48% and 49%, and a pause that let the cache expire was 7% and 17%. Cutting every tool result to 1,000 tokens had a ceiling of
4% and 8%. So a filter on command output, however good, cannot save most of the money, and the larger levers are elsewhere
([findings](../findings.md)).

## Costs

- The first release does not reduce anything. It tells the user where to look.
- The measurement is of one person's work. The tool is built so that anyone can run it on theirs, which is the point.
- The numbers had to be believed before they could be used, and that took several corrections (the price model, the fit, the
  cache rebuilds). [Validation](../validation.md) lists them.

## Alternatives considered

- **Build the output filter first.** It is the obvious move and the cheapest to explain. It would have been built before it was
  known to address a tenth of the cost.
- **Build a hand-off note first.** It has the larger ceiling, and it is also the one whose quality is hardest to show, so it
  needs the measurement and the checking harness more, not less.
