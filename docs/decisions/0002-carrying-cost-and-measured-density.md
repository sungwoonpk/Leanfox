# 0002. The cost of carrying is the unit, and the density of each kind of text is measured per session

Status: accepted

## Context

To say what a piece of text cost, two things are needed: how many tokens it is, and how many requests had it in the
conversation. A session file has neither the tokens of a piece of text nor a tokenizer. It does have, for every request, the number
of tokens the model read, and the size in characters of every piece of text.

## Decision

- The cost of a piece is `tokens x (write price + (requests that carried it - 1) x read price)`. The prices are what the
  session actually paid per token, taken from its own requests, so a change of prices or cache lifetime is picked up without
  editing a table.
- The tokens of a piece are `rate x size`, and the rate comes from the data: the size of the conversation at request k is the
  base plus the sum over kinds of rate times the amount added so far. The rates are the non-negative least-squares solution over
  all requests, solved exactly.
- Each kind of text has its own rate: your messages, program-added reminders, Claude's replies, reasoning, tool calls, tool
  results. Reasoning is counted in blocks, because the file keeps almost none of its text.
- A weak prior, worth a few requests of data, keeps a very short session from producing nonsense. A long session ignores it.
- The fit error (the median relative error of the rebuilt context) is printed, so that the reader can see how far to trust the
  split.
- Requests that wrote far more to the cache than the conversation grew by are cache rebuilds, and are priced and reported as
  their own line.

## Costs

- A rate per kind is a fit, not a fact. Kinds that grow in step (replies and reasoning) cannot be told apart well, and the report
  says that the large groups are reliable and the small ones less so.
- The rebuild rule has a threshold (2,000 tokens) and assumes that a large write with no growth is a rebuild.
- A new model with a different cache lifetime or price is priced by the table, and the table is as old as the commit.

## Alternatives considered

- **A tokenizer.** It would give exact token counts for text, and it is a dependency that would be wrong for the model in use
  whenever the model changed. It also could not see reasoning, which is carried and of which the file has almost nothing.
- **One density for all text.** It was the first version. It fitted worse, and it let a kind of record that is not sent to the
  model (a snapshot of the prompt, written to the file for bookkeeping) distort every other kind.
- **Fixed characters per token.** The measured densities range from 0.3 to 0.8 tokens per character depending on the kind and the
  session, so any single number would have been wrong by more than the effects being measured.
- **Coordinate descent for the fit.** It was tried first. On exact synthetic data it reproduced the total and not the rates.
