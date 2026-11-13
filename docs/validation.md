# Validation

What was checked against real session files, how, and what was not.

## What was checked

The author's local Claude Code history: 7 session files with requests, 6 of them with a cost that Claude Code recorded, from
Claude Sonnet 5.5, Claude Opus 5.5 and Claude Haiku 5.5.

| Check | Result |
| --- | --- |
| The price table, given the tokens Claude Code recorded, against the cost it recorded | Equal to the last stored digit (a relative difference under 1e-13%) for Sonnet 5.5 on 3 sessions, Opus 5.5 on 2, and Haiku 5.5 on 5 (input, output and web search) |
| The size of the conversation at each request, rebuilt from the measured rates | Median error 0.1% to 3.6%; 1.4% and 1.6% on the two long sessions |
| What the main conversation paid for input, against the sum of the parts | Within 1.5% on every session with a recorded cost, once cache rebuilds and a warm start are counted |
| Requests in the file, against what Claude Code recorded | The two long sessions: 99.6% and above of the recorded cache reads are in the file |

Anyone with Claude Code history can repeat the first check: `leanfox report` prints a `check:` line for every session with a
recorded cost.

## How the check found bugs

Each of these was found by comparing with the recorded numbers, and each is in the history of this repository.

- **Counting requests once.** A request is written as one line per content block, with the usage repeated. Adding up every line
  counted more than twice the real tokens. The reader counts each request id once.
- **Pauses.** Before cache rebuilds were counted, 7% and 17% of the input cost on the two long sessions was unexplained. A request that
  writes a lot more than the conversation grew by is a cache that expired in a pause.
- **A warm start.** Two sessions of four and five requests had an explained cost of twice the real one. They started with the system
  prompt already in the cache, and the base was priced as a write. The first request now says which it was.
- **The solver.** A coordinate-descent solver gave plausible totals and wrong densities on exact synthetic data. It was replaced
  by an exact active-set solver that is compared with an exhaustive search on 300 random problems.

## Tests

- Unit tests with 99%+ coverage, a gate of 90% in CI.
- **Mutation checking.** 43 one-line breakages of the price table, the reader, the carry formula, the rebuild rule, both
  simulations, the command classifier and the report (`scripts/mutation-check.py`). The first run, of 41, missed 5. Four got tests
  (result size in bytes, a dip in the context, the cost of carrying the base, a filter that matched anywhere in a name). The fifth, a
  comparison at a measured size exactly equal to a limit, behaves the same either way and no test can tell the two apart. The two
  added with the warm-start fix were caught at once.
- The example session is made by a generator in a test, and the committed file and the README sample are checked against it.
- The program is built and run as a separate process on a small file.

## What was not checked

- **Other agents.** Only Claude Code's session format is read, and only the version of it that these files were written by.
- **Prices outside the table.** The cache rates of Haiku 5.5 and the cache write rates of Fable 5.2 are not verified, because no
  recorded session used them. They are marked so in the table, and the report says when a check involves them.
- **Long prompts on Haiku 5.5.** Prompts over 100,000 tokens cost more per token on that model, and the table does not model it.
- **Other people's sessions.** Everything above is one person's work. A session that looks different could expose a case that none
  of these did.
- **Whether acting on a ceiling is safe.** See [findings](findings.md).
