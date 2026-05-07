# Changelog

All notable user-facing changes to ServersUp Backend will be documented in this file.

This changelog is intended to be readable for end users and can be published directly to the project website.

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
- **Role name resolution (best-effort)**:
  - When configured with a Discord bot token, role display names are resolved and stored for more readable output.

### Changed
- **Discord commands**:
  - `/games` and `/servers` are no longer exposed; discovery happens through `/subscribe` autocomplete and `/subscriptions`.

### Notes
- Backend services are deployed as AWS Lambda functions and use DynamoDB and S3 for storage/configuration. Configuration and infrastructure are managed in a separate Terraform repo.

