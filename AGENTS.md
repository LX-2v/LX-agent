# Repository Guidelines

## Project Structure & Module Organization

LX-agent is a Go 1.27+ stdio MCP server. The executable entry point is `cmd/lx-agent`; implementation packages live under `internal/`: `analyze/pprof` handles profiles, `analyze/goroutines` parses goroutine dumps, `server` exposes MCP tools, and `store` manages the bounded in-memory profile cache. Tests sit beside the packages they cover (`*_test.go`). `docs/` contains tool contracts and project research; `testdata/` contains profile and goroutine fixtures. Keep new implementation details in the narrowest relevant internal package.

## Build, Test, and Development Commands

- `go build ./cmd/lx-agent` builds the server executable.
- `go test ./...` runs all package tests.
- `go test -race ./...` checks for data races.
- `go vet ./...` runs Go's static checks.
- `gofmt -w <files.go>` formats edited Go files.
- `go run ./cmd/lx-agent` starts the MCP server over stdio; configure the documented limits with flags when needed.

The project is offline and read-only at runtime: preserve bounded input, decompression, memory, and output behavior when adding tools or parsers.

## Coding Style & Naming Conventions

Follow standard Go conventions and keep all Go code `gofmt`-formatted. Use short, descriptive, mixed-case identifiers; exported declarations need clear GoDoc comments. Keep packages focused and avoid unnecessary dependencies. Name tests `Test...` and test files `<package>_test.go`. Return actionable errors and honor request cancellation in analysis paths.

## Testing Guidelines

Add focused unit tests beside the code they exercise. Cover malformed and boundary-sized inputs, truncation limits, cancellation, and store eviction where relevant. Use the existing fixtures in `testdata/` for profile and goroutine parsing. Run `go test ./...` after changes; no repository-specific coverage threshold is defined.

## Commit & Pull Request Guidelines

Recent commits use Conventional Commit prefixes such as `feat:`, `chore:`, and `fix:`; follow that pattern with a concise imperative summary (for example, `fix: cap decoded profile size`). Pull requests should explain the behavior change and rationale, link related issues when available, list validation commands and results, and update `docs/tools.md` when tool schemas, defaults, or limits change. Include examples or screenshots only when they clarify a user-visible change.

## Security & Configuration

Treat profile files as untrusted input. Keep parsing in process, enforce configured size limits, and do not add network access, subprocess execution, or file modification to analysis tools. Document any new flags and their defaults.

## Permissions

Global rule:

- Ask the user first before making any code change.
- Show the intended change for review when possible.
- Wait for the user to accept or reject the change before editing project code, never change code else take premition from me.
- Always keep the changes in the code to be as simple as possible, straight to the point, with no comments added

### Allow

- Read tracked project files needed for the task.
- Read source code under `src/`, configuration under `config/`, and docs such as `README.md`, `CLAUDE.md`, and this file.
- Create new source or documentation files when they are required for the requested change.
- Edit application code, route files, models, security, utilities, tests, and markdown documentation.
- Update `package.json` when the task explicitly requires script or dependency changes.
- Run safe repo-local commands such as `rg`, `ls`, `sed`, `git status`, `pnpm lint`, and `pnpm test`.

### Deny

- Do not read, print, or copy secrets from `.env`, `.env.dev`, or any credential file.
- Do not modify `.env`, `.env.dev`, or other secret-bearing files unless the user explicitly asks.
- Do not modify `node_modules/`, generated caches, or log files.
- Do not change `pnpm-lock.yaml` unless dependency work is part of the task.
- Do not delete files, rename major directories, or rewrite large parts of the codebase without explicit approval.
- Do not run destructive git or shell commands such as `git reset --hard`, `git checkout --`, or broad `rm` operations.
- Do not alter deployment/infrastructure files (`Dockerfile`, `docker-compose.yml`, `ecosystem.config.cjs`) unless the task explicitly requires it.
