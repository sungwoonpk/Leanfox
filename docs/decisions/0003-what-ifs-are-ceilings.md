# 0003. The what-ifs are ceilings, and are never presented as savings

Status: accepted

## Context

The report can simulate two changes on a measured session: cutting every tool result to a size, and restarting the conversation
every N requests from a short note. Each gives a dollar figure that is easy to read as "you would save this".

## Decision

Both are labeled ceilings wherever they appear, in the text report, in the documents and in the README. The text says what
each assumes: the agent takes the same steps, what was cut or left out was never needed again, and the note held everything that
mattered. The restart simulation also shows a loss as a negative number instead of hiding it.

## Costs

- The figures look less impressive than "you would save 44%". They are also not wrong.
- Someone who wants a number to act on gets a bound and a reason, and has to decide for themselves.

## Alternatives considered

- **Present them as estimated savings.** It would be accurate only if an agent did exactly as well with less in front of it, and
  nothing here measures that. This project makes no model calls, needs no key and has no way to compare the work an agent does.
  Presenting a bound as an estimate would have been the one dishonest thing in an otherwise careful report.
- **Leave the simulations out.** They are the reason the measurement matters: they say whether a reduction could pay at all, and
  on the sessions measured they say that one kind could not.
