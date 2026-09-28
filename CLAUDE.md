# GD++

CLI for managing Godot projects written in C++: generates boilerplate, has a small built-in build system.

The tool itself is written in Go. C++ is only the language of the Godot projects it manages.

## Design principles

1. Batteries included: like Godot's "all in one" approach, every feature ships in a single CLI tool.
2. Minimal friction: the tool should be a joy to use; a new project can be set up in 1-2 commands.
3. Moderate configurability: users can adapt the tooling to their needs a little, but high configurability is not a goal, since it would conflict with the other principles.

## Guidelines

- Code: as short as possible.
- Comments: only where code is unclear, plus a short header on widely used functions.
- Docs: simple English, plain logic.
- Log messages: capitalized sentences ending with a period (prompts end with `?`; `Check` messages have no period, since `Check` appends the error and a period).
- Always minimize cognitive load.

## Mandatory rules for AI agents (always obey, no exceptions)

1. You are not allowed to access git under any circumstances. Only the humans are allowed to perform operations on the git repository.
