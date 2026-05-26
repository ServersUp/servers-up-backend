# Configuration model

## Server catalog (`server-mapping.json`)

Discord commands (`/subscribe`, `/games`, `/servers`, `/status`, autocomplete, notification labels) use **`server-mapping.json`** in S3 (`CONFIG_BUCKET` / `SERVER_MAPPING_PATH`).

### JSON shape

```json
{
  "games": {
    "wow": {
      "provider": "battlenet",
      "regions": {
        "us": { "servers": { "illidan": { "identifier": 57 } } },
        "eu": { "servers": { "kazzak": { "identifier": 1305 }, "argent-dawn": { "identifier": 1391 } } },
        "kr": { "servers": { "azshara": { "identifier": 214 } } },
        "tw": { "servers": { "skywall": { "identifier": 967 } } }
      }
    }
  }
}
```

Each game has a `provider` string and a `regions` map. Each region entry has a `servers` map whose keys are slug-normalized server names and whose values contain an `identifier` (for Battle.net, the connected realm ID). Region is encoded in the structure rather than per-server.

The canonical server ID written by pollers and stored in DynamoDB is `provider#region#identifier` (e.g. `battlenet#eu#1305`).

### Discord slash commands

`/subscribe`, `/status`, and `/servers` all accept a **`region`** option (`us`, `eu`, `kr`, `tw`) in addition to `game` and `server`. Autocomplete for `region` uses `ListRegions(game)` and autocomplete for `server` requires both `game` and `region` to be set first.

The display label (`ServerLabel`) stored on subscriptions and shown in notifications is `game-region-server` (e.g. `wow-eu-kazzak`).

Register slash commands with Discord’s API: see [`discord-global-commands.md`](discord-global-commands.md) and [`discord-global-commands.json`](discord-global-commands.json).

## Battle.net polling config (separate today)

Regional Battle.net poller Lambdas share one entrypoint ([`bnet-polling-function`](../cmd/bnet-polling-function/)) over [`internal/bnetpoller`](../internal/bnetpoller/). The cmd calls `bnetpoller.LoadFromEnv` to read env vars, wire AWS clients, and resolve SSM secrets, then starts the handler. CI builds once and deploys the same binary to `BNetPollingLambda`, `BNetPollingLambdaEU`, `BNetPollingLambdaKR`, and `BNetPollingLambdaTW` (see `function_names` in `deployment-config.yaml`). Each AWS function loads a **separate** S3 JSON (`BNET_SERVER_CONFIG_PATH`) with region, locale, and `realms[]`—Terraform sets a distinct path per function. Status is written to the status DynamoDB table (`DDB_TABLE_NAME`).

## Future: unified catalog

When additional poller Lambdas exist (API, scraper, pinger), the intended model is:

- **`server-mapping.json`** remains the single catalog of monitored servers.
- Each poller filters entries where `provider` matches its integration and polls only those targets.

Unifying BNet polling with the mapping catalog is deferred until that multi-poller layout is designed.
