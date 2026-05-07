# Registering Discord application (slash) commands

Slash commands are registered with the Discord API. After deploying bot code that supports autocomplete, re-register commands so the `game` and `server` options use **`"autocomplete": true`** (STRING options only). The optional **`role`** option stays type **`8`** (ROLE) and must not use autocomplete.

Replace:

- `APPLICATION_ID` — Discord application (bot) ID  
- `BOT_TOKEN` — bot token with `applications.commands` scope  
- `GUILD_ID` — target guild (guild-scoped commands), or omit the guild segment for global commands  

## Guild commands (example)

```bash
curl -sS -X PUT "https://discord.com/api/v10/applications/APPLICATION_ID/guilds/GUILD_ID/commands" \
  -H "Authorization: Bot BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d @commands.json
```

Put this JSON in `commands.json` (array of command definitions):

```json
[
  {
    "name": "subscribe",
    "type": 1,
    "description": "Subscribe this channel to server status updates",
    "options": [
      {
        "name": "game",
        "description": "Game id (type to search)",
        "type": 3,
        "required": true,
        "autocomplete": true
      },
      {
        "name": "server",
        "description": "Server id for that game (type to search)",
        "type": 3,
        "required": true,
        "autocomplete": true
      },
      {
        "name": "role",
        "description": "Optional role to mention on alerts",
        "type": 8,
        "required": false
      }
    ]
  },
  {
    "name": "unsubscribe",
    "type": 1,
    "description": "Unsubscribe this channel from a server",
    "options": [
      {
        "name": "game",
        "description": "Game id (type to search)",
        "type": 3,
        "required": true,
        "autocomplete": true
      },
      {
        "name": "server",
        "description": "Server id for that game (type to search)",
        "type": 3,
        "required": true,
        "autocomplete": true
      }
    ]
  }
]
```

Add your other commands (`help`, `games`, `servers`, `subscriptions`) to the same array as needed, or register them in a separate PUT with the full desired command list for that application/guild.

**Note:** `PUT` replaces the entire command set for that application (guild or global). Include every command the bot should expose in one payload, or use `PATCH` per command id for incremental updates.
