# Security

## Reporting a problem

Please **do not open a public issue** for a security problem. Use GitHub's private reporting instead: on the repository page,
open the **Security** tab and choose **Report a vulnerability**. Include what you found, how to reproduce it and what you think
the impact is.

This is a personal project, so there is no service level. Reports are read and answered on a best-effort basis, and a confirmed
problem is fixed in `main`. Only the latest `main` is supported.

## What it does with your files

Leanfox reads session files and prints sizes, counts and the kind of each command. It has no networking code (a test fails if
any is linked), writes nothing, and never prints command arguments or folder names.
[docs/privacy-and-safety.md](docs/privacy-and-safety.md) lists what it reads, what it prints, and how it treats damaged or
crafted files. Lean mode's hook runs on every prompt while the plugin is installed: it keeps a few small private files under
`~/.claude/leanfox`, never blocks a prompt, and never changes what a tool returns. It has not had an independent security review.
