# GD++

CLI for managing Godot projects written in C++: generates boilerplate, has a small built-in build system.

The tool itself is written in Go. C++ is only the language of the Godot projects it manages.

The Go module lives in `src/`; the paths below are relative to it. `make test` runs `go vet` and all tests.

## Design principles

1. Batteries included: like Godot's "all in one" approach, every feature ships in a single CLI tool.
2. Minimal friction: the tool should be a joy to use; a new project can be set up in 1-2 commands.
3. Moderate configurability: users can adapt the tooling to their needs a little, but high configurability is not a goal, since it would conflict with the other principles.

## Guidelines

- Code: as short as possible.
- Comments: only where code is unclear, plus a short header on widely used functions.
- Docs: simple English, plain logic.
- Log messages: capitalized sentences ending with a period or `!` (prompts end with `?`; `Check` messages have no period, since `Check` appends the error and a period).
- Commands: a successful run that modified the system ends with a single `LogInfo("Success!")`, not a summary of what was done. Exceptions: `build` and `clean`, whose per-package `Building ...`/`Cleaning ...` lines already say it all.
- Commands: `cmd_*.go` files never depend on each other; helpers shared between commands live in non-command files (e.g. `utils.go`).
- Commands: keep the commands in `main.go` sorted alphabetically, both in the `Args` definitions and in the `switch` cases.
- Paths: never interpolate a Path's raw absolute path (`p.absolutePath`) into a log, `Assert`, or `Check` message; use `p.ToString()` instead, so messages show a `res://`-relative or cwd-relative path rather than leaking the user's filesystem layout. Exception: functions on `ToString()`'s own dependency path (`stat`, `Exists`, `IsDir`, `IsFile`, and `ToString()` itself) must not call `p.ToString()` in their own messages, since Go evaluates call arguments eagerly and that would recurse forever; they omit the path instead.
- C++ rewrites: GD++ extends C++ with a few rewrites, words that it turns into plain C++ (see `cpp` in `trans/syntax_N/writer.go` and `man/lang_N/rewrites.txt`). They're used sparingly: add one only when a human user decides to, keep it trivial, and add its word to `rewriteWords` or `rewriteOperatorWords` in `highlight.go`, so it's highlighted.
- Language changes: every change to GD++'s syntax, including C++ rewrites, must update (not limited to): the manual pages that mention it (`man/`, where `man/lang_0.txt` and `man/lang_0/` are the language pages of syntax 0, and `man/tut_0.txt` and `man/tut_0/` its tutorials; grep for the old form), the highlighting (`highlight.go`), and the tests (`writer_test.go`, `highlight_test.go`, the goldens in `trans/syntax_N/testdata`).
- Syntax forks: `trans/syntax_0` is nightly, so breaking changes are fine and no backward compatibility is needed. `trans/syntax_N` for N>0 are stable: they can be edited, e.g. to fix bugs or improve generated code, but never in a way that breaks backward compatibility, i.e. code that worked must keep working the same way. The same goes for their language manuals (`man/lang_N.txt`, `man/lang_N/`, and their list in `langManPages`) and tutorials (`man/tut_N.txt`, `man/tut_N/`, and their list in `tutManPages`): they can be edited, but must keep describing the same language. A fork registers in `trans/trans.go`; `LatestSyntax` there is the default for new packages.
- Always minimize cognitive load.

## Mandatory rules for AI agents (always obey, no exceptions)

1. You are not allowed to access git under any circumstances. Only the humans are allowed to perform operations on the git repository.
