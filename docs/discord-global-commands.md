# Discord global slash commands (registration)

Register commands with the Discord REST API after deploying bot code that handles them.

Replace:

- `YOUR_APPLICATION_ID` — Discord application (bot) ID
- `YOUR_BOT_TOKEN` — bot token (not the public key)

```bash
export DISCORD_APPLICATION_ID="YOUR_APPLICATION_ID"
export DISCORD_BOT_TOKEN="YOUR_BOT_TOKEN"
```

## Register all global commands (PUT)

`PUT` replaces the full global command list.

```bash
curl -sS -X PUT \
  "https://discord.com/api/v10/applications/${DISCORD_APPLICATION_ID}/commands" \
  -H "Authorization: Bot ${DISCORD_BOT_TOKEN}" \
  -H "Content-Type: application/json" \
  -d @docs/discord-global-commands.json
```

## Command payload

See [`discord-global-commands.json`](discord-global-commands.json) in this directory.

**Region option** — `region` is a string with autocomplete; allowed values come from `server-mapping.json` per game (WoW: `us`, `eu`, `kr`, `tw`; FFXIV: `na`, `eu`, `jp`, `oce`). The JSON `description` fields are hints only; run the PUT above after changing them.

## Verify

```bash
curl -sS \
  "https://discord.com/api/v10/applications/${DISCORD_APPLICATION_ID}/commands" \
  -H "Authorization: Bot ${DISCORD_BOT_TOKEN}" | jq .
```

Global commands can take up to an hour to propagate; use guild commands for faster testing in a dev server if needed.
