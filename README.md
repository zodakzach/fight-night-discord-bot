# Fight Night Discord Bot

A small, focused Discord bot that posts MMA fight-night updates to your server. It uses a modular source system (ESPN for UFC today) so as more orgs are added, the bot can fetch from different providers. Pick your org, choose a channel, and get one clean post per event.

## Why
I'm lazy and tired of manually posting fight-night events in my Discord server. I announce fights and share my picks, and keeping up with those event posts is a chore — so I made this bot to announce the fight nights for me.

## What
- Notifies a configured channel on fight nights for your chosen org (UFC supported now).
- Lets you select the org and destination channel for posts.
- Provides a quick "next event" lookup command.
- Tracks last-posted event per guild and per org to prevent duplicates.

## Features
- Org selection per guild (UFC supported today; others later).
- Notifications are OFF by default; you must set an org before enabling.
- Channel routing to a specific, configurable channel.
- Event-day posting with at-most-once delivery per event/guild/org.
- Next-event lookup via slash command.

## Commands
Representative commands (names/args may vary slightly based on current implementation):
- `/set-org org:<ufc>`: Choose the organization (currently UFC only). Required before enabling notifications.
- `/set-channel channel:<#channel>`: Pick the channel for notifications.
- `/notify on|off`: Enable or disable fight-night posts for this guild (requires org set; default is off).
- `/next-event`: Show the next event for the selected org.
- `/set-tz tz:<Region/City>`: Set the guild timezone (IANA name).
- `/status`: Show current settings for this guild.
- `/help`: Show available commands and usage.

## Getting Started
- Set org: run `/set-org org:<ufc>`.
- Pick channel: run `/set-channel channel:<#your-channel>`.
- Optional timezone: run `/set-tz tz:<Region/City>` (defaults to `TZ` env).
- Enable notifications: run `/notify on` (notifications are off by default).
- Verify: run `/next-event` to see the next event for your org.

Notes
- Posts run daily at `RUN_AT` in your configured timezone; event-day posts only.
- You must set an org before enabling notifications.

## Tech Stack
- Language: Go 1.25
- Discord: `discordgo` slash commands and interactions
- Sources: modular provider interface; UFC via ESPN scraping client
- Scheduling: daily scheduler, IANA timezone via `TZ`
- State: persistent store for guild config and last-posted
- Config: environment-first with `.env` via `godotenv`
- Tests: standard `testing` with table-driven cases

## Project Structure
- `cmd/fight-night-bot/main.go`: Entrypoint; wires config, Discord, sources manager, scheduler; graceful shutdown on SIGINT/SIGTERM.
- `internal/config`: Loads env (`.env` via `godotenv`), defaults, required vars.
- `internal/discord`: Slash commands and daily notifier scheduling/handlers.
- `internal/sources`: Provider interfaces and registry for org-specific event sources.
- `internal/espn`: Scraper client used by the UFC provider.
- `internal/state`: Guild settings and last-posted state (SQLite).
- `.env` (local only) and state storage (see `internal/state`).

## Database Migrations

- Engine: SQLite via CGO (`github.com/mattn/go-sqlite3`).
- Migrations: `golang-migrate` with embedded SQL, auto-run at boot.
  - Files live under `internal/migrate/migrations/` and are embedded into the binary.
  - On application startup, migrations run automatically before the DB is used.
  - This works well on Fly.io where `release_command` cannot access the volume.

Adding a migration:

- Create two files with the next sequence number (e.g., `0002_add_new_table.up.sql` and `0002_add_new_table.down.sql`) in `internal/migrate/migrations/`.
- Keep migrations deterministic and forward-only; use `IF NOT EXISTS` only when bootstrapping compatibility is required.
- Build and run the bot; it will apply the new migration on startup.

## Configuration
- Required:
  - `DISCORD_TOKEN`: Discord bot token
- Optional:
  - `GUILD_ID`: Dev guild for command registration
  - `RUN_AT`: Daily run time `HH:MM` (e.g., `16:00`)
  - `TZ`: IANA timezone (e.g., `America/New_York`)
  - `DB_FILE`: SQLite database path (default `state.db`; Docker runtime defaults to `/data/bot.db`)
  - `LOG_LEVEL`: `debug` | `info` | `warn` | `error` (default `info`)

Example `.env`:
```
DISCORD_TOKEN=your-bot-token
GUILD_ID=123456789012345678
RUN_AT=16:00
TZ=America/New_York
LOG_LEVEL=info
```

## Build, Run, Test
- Run locally: `go run ./cmd/fight-night-bot`
- Build binary: `go build -o bin/fight-night-bot ./cmd/fight-night-bot`
- Format/Vet: `go fmt ./... && go vet ./...`
- Tests: `go test -race -cover ./...`

## Deploy (Fly.io)
Target: containerized deploy with a small persistent volume for SQLite.

- Secrets:
  - `fly secrets set DISCORD_TOKEN=...`
  - Optional: `fly secrets set TZ=America/New_York RUN_AT=16:00 USER_AGENT='your-agent'`
- Volume (for SQLite):
  - `fly volumes create data --size 1 --region <region>`
  - Mounted at `/data` via `fly.toml`; Dockerfile defaults `DB_FILE=/data/bot.db` already
- Dockerfile: multi-stage build on Go 1.25, copy binary to a minimal runtime, run as non-root, includes time zone data.
- `fly.toml`: define `[env]` for `TZ`/`RUN_AT` and add `[[mounts]]` for `/data`.

## Roadmap
- Sources: add more orgs (Bellator, PFL, ONE) via providers; health checks and fallbacks per provider.
- Notifications: per-guild `RUN_AT`; per-org toggles; improved de-duplication by event ID and windowing.
- Messaging: rich embeds (title, banner, links), start-time summaries, prelim/main-card breakdowns.
- Reliability: HTTP retries with backoff, rate limiting, basic response caching, stricter time parsing.
- Scheduling: DST edge-case tests and fixes; year-boundary range correctness; resilience against clock drift.
- Ops/CI: Dockerfile, GitHub Actions for fmt/vet/test, release artifacts; lightweight observability/structured logs.
  - Logs: JSON-structured via Go `slog`; compatible with Fly.io log ingestion. Control verbosity with `LOG_LEVEL`.
- Security: least-privilege bot permissions, secret handling guidance, user-agent customization knob.

## Contributing
- Try it in a dev guild and share behavior logs or screenshots.
- Suggest command UX and defaults (times, mentions, channel selection).
- Report ESPN payload quirks to harden parsing and messaging.
- PRs welcome: keep diffs focused; ensure `go fmt`, `go vet`, and tests pass.
