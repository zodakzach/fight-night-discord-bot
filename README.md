# Fight Night Discord Bot

A small, focused Discord bot that posts MMA fight-night updates to your server using ESPN’s UFC scoreboard. Pick your org (UFC today), choose a channel, and get one clean post per even.

## Why
I'm lazy and tired of manually posting fight-night events in my Discord server. I announce fights and share my picks, and keeping up with those event posts is a chore — so I made this bot to announce the fight nights for me.

## What
- Notifies a configured channel on fight nights for your chosen org (UFC supported now).
- Lets you select the org and destination channel for posts.
- Provides a quick "next event" lookup command.
- Tracks last-posted event per guild to prevent duplicates.

## Features
- Org selection per guild (UFC supported today; others later).
- Channel routing to a specific, configurable channel.
- Event-day posting with at-most-once delivery per event/guild.
- Next-event lookup via slash command.

## Commands
Representative commands (names/args may vary slightly based on current implementation):
- `/notify on|off`: Enable or disable fight-night posts for this guild.
- `/set-org org:<ufc>`: Choose the organization (currently UFC only).
- `/set-channel channel:<#channel>`: Pick the channel for notifications.
- `/next-event`: Show the next event for the selected org.
- `/set-tz tz:<Region/City>`: Set the guild timezone (IANA name).
- `/status`: Show current settings for this guild.
- `/help`: Show available commands and usage.

## Tech Stack
- Language: Go 1.25
- Discord: `discordgo` slash commands and interactions
- HTTP: stdlib `net/http` to ESPN UFC scoreboard
- Scheduling: daily scheduler, IANA timezone via `TZ`
- State (current): JSON file (`STATE_FILE`, default `state.json`)
- State (planned): SQLite + `sqlx` for durable, concurrent-safe storage
- Config: environment-first with `.env` via `godotenv`
- Tests: standard `testing` with table-driven cases

## Project Structure
- `cmd/fight-night-bot/main.go`: Entrypoint; wires config, Discord, ESPN client, scheduler.
- `internal/config`: Loads env (`.env` via `godotenv`), defaults, required vars.
- `internal/discord`: Slash commands and daily notifier scheduling/handlers.
- `internal/espn`: Thin HTTP client for ESPN UFC scoreboard API.
- `internal/state`: JSON-backed guild settings and last-posted state.
- `.env` (local only) and `state.json` (runtime state; default `state.json`).

## Configuration
- Required:
  - `DISCORD_TOKEN`: Discord bot token
- Optional:
  - `GUILD_ID`: Dev guild for command registration
  - `RUN_AT`: Daily run time `HH:MM` (e.g., `16:00`)
  - `TZ`: IANA timezone (e.g., `America/New_York`)
  - `STATE_FILE`: Path for JSON state (default `state.json`)
  - Planned: `DB_PATH`: SQLite path (e.g., `/data/bot.db` on Fly.io)

Example `.env`:
```
DISCORD_TOKEN=your-bot-token
GUILD_ID=123456789012345678
RUN_AT=16:00
TZ=America/New_York
```

## Build, Run, Test
- Run locally: `go run ./cmd/fight-night-bot`
- Build binary: `go build -o bin/fight-night-bot ./cmd/fight-night-bot`
- Format/Vet: `go fmt ./... && go vet ./...`
- Tests: `go test -race -cover ./...`

## Deploy (Fly.io)
Target: containerized deploy, with a small persistent volume for SQLite once migrated.

- Secrets:
  - `fly secrets set DISCORD_TOKEN=...`
  - Optional: `fly secrets set TZ=America/New_York RUN_AT=16:00`
- Volume (for SQLite, planned):
  - `fly volumes create data --size 1`
  - Mount at `/data` and set `DB_PATH=/data/bot.db`
- Dockerfile: multi-stage build on Go 1.25, copy binary to a minimal runtime, run as non-root.
- `fly.toml`: define `[env]` for `TZ`/`RUN_AT`, and `[mounts]` for `/data` once SQLite is used.

Note: If you want, we can add a Dockerfile and example `fly.toml` to streamline deploys.

## Roadmap
- Storage: migrate state to SQLite + `sqlx` with simple migrations and file locking.
- Commands: add `/status`, `/help`, per-guild channel/roles, per-guild `RUN_AT`/timezone.
- Messaging: richer embeds (event card, start times), prelim/main-card details.
- Reliability: retries/backoff for ESPN, request throttling, clearer operational logs.
- Scheduling: event-day-only improvements, DST hardening across timezones.
- Ops/CI: Docker image, release workflow, fmt/vet/tests in CI.
- Security: least-privilege bot permissions, secret handling guidance.

## Contributing
- Try it in a dev guild and share behavior logs or screenshots.
- Suggest command UX and defaults (times, mentions, channel selection).
- Report ESPN payload quirks to harden parsing and messaging.
- PRs welcome: keep diffs focused; ensure `go fmt`, `go vet`, and tests pass.
