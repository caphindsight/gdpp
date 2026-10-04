# GD++

A CLI for Godot projects written in C++. It generates boilerplate, and has a small built-in build system.

- The tool is written in Go. C++ is only the language of the Godot projects it manages.
- The Go module is in `src/`. The paths below are relative to it.
- `make test` runs `go vet` and all tests.

## Design principles

1. Batteries included: like Godot's "all in one" approach, every feature ships in one CLI tool.
2. Minimal friction: the tool is a joy to use. A new project takes 1-2 commands to set up.
3. Moderate configurability: users can adapt the tool a little. High configurability is not a goal, since it conflicts with the other principles.

## Guidelines

General:

- Always minimize cognitive load.
- Code: as short as possible.
- Comments: only where code is unclear, plus a short header on widely used functions.
- Docs: simple English, plain logic.
  - Concise sentences, always to the point.
  - Prefer lists of facts to long paragraphs.
  - Don't call syntax elements "words": say keyword, name, identifier or rewrite.

Log messages:

- Capitalized sentences, ending with a period or `!`.
- Prompts end with `?`.
- `Check` messages have no period: `Check` appends the error and a period.

Commands:

- A successful run that changed the system ends with one `LogInfo("Success!")`, not a summary of what was done.
  - Exceptions: `build` and `clean`. Their per-package `Building ...` and `Cleaning ...` lines already say it all.
- `cmd_*.go` files never depend on each other. Helpers shared between commands live in non-command files, e.g. `utils.go`.
- Keep the commands in `main.go` sorted alphabetically, in the `Args` definitions and in the `switch` cases.

Paths:

- Never put a Path's raw absolute path (`p.absolutePath`) into a log, `Assert` or `Check` message.
- Use `p.ToString()` instead. Messages then show a `res://`-relative or cwd-relative path, and don't leak the user's filesystem layout.
- Exception: the functions that `ToString()` itself depends on (`stat`, `Exists`, `IsDir`, `IsFile` and `ToString()`) must not call `p.ToString()` in their own messages. Go evaluates arguments eagerly, so that would recurse forever. They omit the path instead.

C++ rewrites:

- GD++ extends C++ with a few rewrites, which it turns into plain C++. See `cpp` in `trans/syntax_N/writer.go`, and `man/lang_N/rewrites.txt`.
- Use them sparingly: add one only when a human user decides to.
- Keep each one trivial.
- Add each one to `rewriteWords` or `rewriteOperatorWords` in `highlight.go`, so it's highlighted.

Language changes:

- Every change to GD++'s syntax, C++ rewrites included, must update at least:
  - the manual pages that mention it, in `man/`. Grep for the old form. Syntax 0's language pages are `man/lang_0.txt` and `man/lang_0/`, and its tutorials `man/tut_0.txt` and `man/tut_0/`.
  - the highlighting, in `highlight.go`,
  - the tests: `writer_test.go`, `highlight_test.go`, and the goldens in `trans/syntax_N/testdata`.

Syntax forks:

- A fork registers in `trans/trans.go`. `LatestSyntax` there is the default for new packages.
- `trans/syntax_0` is nightly: breaking changes are fine, and no backward compatibility is needed.
- `trans/syntax_N` for N>0 are stable:
  - They can be edited, e.g. to fix bugs, improve generated code, or make backward compatible language changes.
  - They must never break backward compatibility: code that worked keeps working the same way.
  - Mostly backward compatible is enough. Rare special cases don't count, e.g. a user's name clashing with a new GD++ name.
  - The same goes for their language manuals (`man/lang_N.txt`, `man/lang_N/` and their list in `langManPages`) and tutorials (`man/tut_N.txt`, `man/tut_N/` and their list in `tutManPages`). They can be edited, but must keep describing a backward compatible language.
- Syntax 1 is a special case: it doesn't provide complete backward compatibility yet.

## Mandatory rules for AI agents (always obey, no exceptions)

1. You are not allowed to access git under any circumstances. Only the humans are allowed to perform operations on the git repository.
