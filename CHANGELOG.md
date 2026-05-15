# Changelog

All notable user-facing changes to ServersUp Backend will be documented in this file.

This changelog is intended to be readable for end users and can be published directly to the project website.

## v1.0.1 — 2026-05-13

### Added
- **`/games`** — list supported games from the configured server mapping (same source as `/subscribe` autocomplete).

## v1.0.0 — 2026-05-07

### Added
- **Discord subscription workflow**:
  - `/subscribe` with game/server autocomplete (type-to-search) and optional role mention.
  - `/unsubscribe` via subscription picker (type-to-search across the guild) using the same entries shown by `/subscriptions`.
  - `/subscriptions` to list all subscriptions in a guild grouped by channel.
  - `/help` for usage, tips, and documentation link.
- **Human-friendly output**:
  - Unsubscribe responses formatted as “Unsubscribed … from **game-server** server status updates in #channel”.
  - Autocomplete options display game/server/role/channel in a readable form.
- **Duplicate subscription protection**:
  - Prevents creating the same channel+server+role subscription twice and returns an “Already subscribed” message with details.

### Changed
- **Discord commands**:
  - `/games` and `/servers` are no longer exposed; discovery happens through `/subscribe` autocomplete and `/subscriptions`.
