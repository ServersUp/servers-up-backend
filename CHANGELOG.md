# Changelog

All notable user-facing changes to ServersUp Backend will be documented in this file.

This changelog is intended to be readable for end users and can be published directly to the project website.

## v1.1 — 2026-05-26

### Added
- **Multi-region World of Warcraft** — the bot supports realms in **US, EU, Korea (KR), and Taiwan (TW)**, not only US. You pick a region when subscribing or checking status so the same realm name in different regions is handled correctly.
- **`/regions`** — list which regions are available for a game (for WoW: `us`, `eu`, `kr`, `tw`).

### Changed
- **`/subscribe`** — adds a required **`region`** option. Autocomplete order is **game → region → server** (type to search at each step). New subscriptions are labeled **`game-region-server`** (e.g. `wow-eu-kazzak`).
- **`/unsubscribe`** — subscription picker and confirmation text use the region-aware label; autocomplete shows **game**, **region**, and **server** when the subscription includes a region.
- **`/subscriptions`** — guild list shows region-aware labels (e.g. `wow-us-illidan`, `wow-eu-kazzak`) so you can tell US and EU (and other) realms apart in the same channel.
- **`/status`** and **`/servers`** — require **`region`** like `/subscribe`; use them to look up or list servers in a specific region.

## v1.0.5 — 2026-05-21

### Fixed
- **Status notifications** now show the server name exactly as it appeared when you subscribed (e.g. `wow-illidan`), rather than reverse-mapping the internal server ID at notify time. Existing subscriptions without a stored label continue to use the fallback mapping as before.

## v1.0.4 — 2026-05-21

### Fixed
- **`/subscribe`** — treats an existing subscription for the same channel and server as a duplicate even when the optional role differs (e.g. channel-wide vs role mention).

## v1.0.3 — 2026-05-15

### Changed
- **`/subscribe`** and **`/unsubscribe`** — only members with **Manage Channels** or **Administrator** can add or remove subscriptions (ephemeral message if denied).

## v1.0.2 — 2026-05-15

### Added
- **`/servers`** — list servers configured for a game (game autocomplete). Long lists link to the [supported games page](https://serversup.github.io/#games); for **wow**, also shows popular US realm names.
- **`/status`** — show the current **UP/DOWN** status for a game + server (same autocomplete as `/subscribe`).

### Changed
- **`/status`** — rate-limited per user and per guild (in-process); repeated checks within a short window get an ephemeral “slow down” message instead of hitting the database every time.

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
