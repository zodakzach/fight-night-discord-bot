# Repository Guidelines

## Project Structure & Modules
- `cmd/fight-night-bot/main.go`: Application entrypoint; wires config, Discord, ESPN client, and scheduler.
- `internal/config`: Loads env (`.env` via `godotenv`), defaults, and required vars.
- `internal/discord`: Slash commands (`/notify`) and daily notifier scheduling/handlers.
- `internal/espn`: Thin HTTP client for ESPN UFC scoreboard API.
- `internal/state`: SQLite-backed guild settings and last-posted state.
- `.env` (local only) and state storage (SQLite database; see `DB_FILE`).

## Build, Run, and Test
- Run locally: `go run ./cmd/fight-night-bot` (requires `DISCORD_TOKEN`).
- Build binary: `go build -o bin/fight-night-bot ./cmd/fight-night-bot`.
- Format/Vet: `go fmt ./... && go vet ./...`.
- Tests (when present): `go test ./...` or with extras: `go test -race -cover ./...`.

## Coding Style & Naming
- Go formatting: run `go fmt ./...` before pushing. Prefer idiomatic Go, clear error handling (`if err != nil { ... }`).
- Packages/files: lowercase, short names (e.g., `state`, `notifier.go`).
- Exports: export only APIs needed by other packages; keep internals unexported.
- Logs: use `log.Printf` for operational messages; avoid panics outside `main`.
- Time: use IANA TZ names; helpers already parse `HH:MM` (`parseHHMM`).

## Testing Guidelines
- Use the standard `testing` package; table-driven tests preferred.
- Place tests next to code in the same package; name with `_test.go` (e.g., `notifier_test.go`).
- Suggested targets: time parsing, scheduling boundaries, state read/write, and message building.
- Run with race detector and coverage: `go test -race -cover ./...`.

## Commit & PR Guidelines
- Commits: concise, imperative subject (e.g., "add notifier retry"), optional Conventional Commits scope is welcome.
- PRs: include summary, rationale, and any screenshots/log snippets for behavior changes. Link related issues.
- Checks: ensure `go fmt`, `go vet`, and tests pass; document env vars touched.

## Security & Configuration
- Required env: `DISCORD_TOKEN`. Optional: `GUILD_ID` (dev guild), `RUN_AT` (HH:MM), `TZ` (IANA), `DB_FILE`, `USER_AGENT`.
- Example `.env`:
  
  ```
  DISCORD_TOKEN=your-bot-token
  GUILD_ID=123456789012345678
  RUN_AT=16:00
  TZ=America/New_York
  ```
- Do not commit secrets. Use `DB_FILE` to point to a local SQLite path that is not tracked by git.
