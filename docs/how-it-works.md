# How it works

## What a session file holds

Claude Code writes one JSON line per event to `~/.claude/projects/<project>/<session>.jsonl`. Leanfox uses four things from it:

- **The usage of each request**: input tokens, output tokens (reasoning included), and the tokens read from and written to the
  prompt cache, with the cache write split by lifetime. A request is written as one line per content block, with the same
  usage repeated in each, so requests are counted once by request id.
- **The content blocks**: what the person typed, what the model wrote (text, reasoning, tool calls), and what the tools returned.
  Only their size in characters is kept, plus a hash of tool results to find identical output.
- **Sub-agent marks**: requests made by sub-agents are priced but are not part of the main conversation.
- **The cost state** that Claude Code writes from time to time: a running total, and per model the tokens and the cost. The
  last one is the final total.

Everything else is ignored. The format is not documented and changes between versions, so the reader asks for as little as it
can ([docs/privacy-and-safety.md](privacy-and-safety.md) lists what it does with lines it cannot read).

## Cost

Each request is priced from the table in `internal/pricing`: input, output, cache read, and cache write at the five-minute and
one-hour lifetimes. The report shows the total that Claude Code recorded and splits it by what it paid for. Then it does the
check that matters: it prices the *recorded tokens* and compares the result with the *recorded cost*. On every session
checked they agreed to the last stored digit ([validation](validation.md)). A model that is not in the table is never guessed at:
its recorded cost stays in the total, is shown as unsplit, and `--rates` can add it.

## The cost of carrying

Every request sends the whole conversation again. A token that enters it is written to the cache once, at the write price, and is
then read at the read price by each later request. So the cost of a piece of text is

    tokens x (write price + (requests that carried it - 1) x read price)

A result of 5,000 tokens that is 200 requests old has been paid for 200 times. Leanfox calls this the cost of carrying. The prices
are not taken from a table here: they are what the session actually paid per token, from the same requests.

## Where the tokens of each kind come from

A session file does not say how many tokens a piece of text is, and there is no tokenizer to ask. It does say how large the
conversation was at every request. Within a stretch of conversation, the size at request k is a fixed base plus what was added
since:

    context[k] - context[0] = sum over kinds of  rate(kind) x amount of that kind added up to request k

The kinds are: your messages, reminders and files added by the program, Claude's replies, reasoning, tool calls, and tool results.
The amount is characters, except for reasoning, which the file keeps almost none of and which is counted in blocks.

The rates are the **non-negative least-squares** fit over every request of every stretch. This is solved exactly (the active set
method of Lawson and Hanson, on scaled normal equations), because the kinds grow together and a method that improves one
coefficient at a time stops long before the answer. A weak prior, worth a few requests of data, keeps a very short session from
producing nonsense. A long session overrules it completely.

The number to look at is the **fit error**: the median relative error when the size of each request is rebuilt from the fitted
rates. It is 1.4% to 1.6% on the long sessions measured. It does not make every rate right: kinds that grow in step, such as
Claude's replies and its reasoning, are hard to tell apart, so the large groups are reliable and the small ones less so.

A stretch ends when the context drops under half of what it was, from at least 20,000 tokens. That is a compaction, a clear, or a
resume, and what was in the conversation before is gone.

## Cache rebuilds

The cache has a lifetime. When a pause outlasts it, the next request writes the whole conversation again. That shows as a request
that wrote far more tokens than the conversation grew by (at least 2,000 more). Leanfox counts those requests and prices the extra
as write instead of read. On the long sessions measured this was 7% and 17% of the input cost, which is why it has its own line.

## What is left over

Add the base, every kind of text carried, and the rebuilds, and compare with what the main conversation paid for input. The
difference is shown only when it is over 2%. On every session with a recorded cost it was under 1.5%.

The base is written once only if the first request had to write it. When an earlier session left the system prompt in the cache,
the first request only reads it, and the base is priced that way. (The first version assumed a write, which put the explained
cost at twice the real one on sessions of a handful of requests.)

## The two what-ifs

Both run on the measured session and both are **ceilings**: they assume the agent takes the same steps and that what was taken
out is never needed again.

- **Cap results.** Every tool result bigger than a limit is cut to it. The saving is the cut tokens times their cost of carrying.
- **Restart.** The conversation is restarted every N requests from the base plus a short hand-off note (3,000 tokens by default,
  `--handoff` sets it). A restart writes its base to the cache instead of reading it, and from then on the conversation is as
  large as the original was, minus everything before the restart. Early in a session this can lose money, and the report shows
  that as a negative number.

[Decision 0003](decisions/0003-what-ifs-are-ceilings.md) says why they are not presented as savings.
