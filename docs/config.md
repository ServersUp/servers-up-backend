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

`/subscribe`, `/status`, and `/servers` all accept a **`region`** option in addition to `game` and `server`. **Regions are per game** (from `games.<id>.regions` in the mapping): WoW uses `us`, `eu`, `kr`, `tw`; FFXIV uses `na`, `eu`, `jp`, `oce`. Autocomplete for `region` calls `ListRegions(game)`; `server` autocomplete requires `game` and `region` first.

The display label (`ServerLabel`) stored on subscriptions and shown in notifications is `game-region-server` (e.g. `wow-eu-kazzak`, `ffxiv-na-gilgamesh`).

Register slash commands with Discord’s API: see [`discord-global-commands.md`](discord-global-commands.md) and [`discord-global-commands.json`](discord-global-commands.json). Re-register when command descriptions change.

## Battle.net polling config (separate today)

Regional Battle.net poller Lambdas share one entrypoint ([`bnet-polling-function`](../cmd/bnet-polling-function/)) over [`internal/bnetpoller`](../internal/bnetpoller/). The cmd calls `bnetpoller.LoadFromEnv` to read env vars, wire AWS clients, and resolve SSM secrets, then starts the handler. CI builds once and deploys the same binary to `BNetPollingLambda`, `BNetPollingLambdaEU`, `BNetPollingLambdaKR`, and `BNetPollingLambdaTW` (see `function_names` in `deployment-config.yaml`). Each AWS function loads a **separate** S3 JSON (`BNET_SERVER_CONFIG_PATH`) with region, locale, and `realms[]`—Terraform sets a distinct path per function. Status is written to the status DynamoDB table (`DDB_TABLE_NAME`).

Example poller file shape (`bnet-servers-config-tw.json`):

```json
{
  "region": "tw",
  "locale": "zh_TW",
  "realms": [
    { "name": "Skywall", "slug": "skywall", "connected_realm_id": 974 }
  ],
  "polling_interval_seconds": 60
}
```

To regenerate poller configs and `server-mapping.json` from Battle.net (localized names per region locale), run the gitignored maintainer tool:

`go run ./scripts/generate-bnet-configs` (from repo root) with `BNET_CLIENT_ID` and `BNET_CLIENT_SECRET` set. It writes `config-out/bnet-servers-config-<region>.json` plus `config-out/server-mapping.json` using [`internal/bnet`](../internal/bnet) (`BuildRealmConfigs`, `DefaultWoWRegionEndpoints`) and [`internal/servermap`](../internal/servermap).

**Ops:** each poll logs **`Poll timing`** (`pollDurationMs`, `bnetApiAvgMs`, `ddbAvgMs`, …). CloudWatch namespace **`ServersUp`**: `PollDurationMs`, `PollBnetApiAvgMs`, `PollBnetApiMaxMs`, `PollRealmSuccess`, `PollRealmError` (dimensions `gameId`, `bnetRegion`). Filter logs with `msg = "Poll timing"`.

## FFXIV config (Lodestone world status)

FFXIV catalog data is generated from the official Lodestone world status page (HTML scrape; no API key). Parser lives in [`internal/ffxivlodestone`](../internal/ffxivlodestone/).

From repo root:

```bash
go run ./scripts/generate-ffxiv-configs
```

**Outputs** under `config-out/`:

- `ffxiv-lodestone-config.json` — poller catalog (`lodestone_url`, `polling_interval_seconds`, `regions` with `worlds[]` of `slug` + exact `name`)
- `server-mapping.json` — adds or updates `games.ffxiv` while **preserving** other games (e.g. `wow`) if the file already exists

**Flags:** `-lodestone-url`, `-game`, `-provider`, `-regions` (default `na,eu,jp,oce`), `-polling-interval`, `-output-dir`, `-mapping-file`, `-mapping-only-ffxiv`, `-dry-run`.

**Conventions:**

- Game id `ffxiv` (no hyphens in game ids).
- Provider `lodestone`; future status `serverId` = `lodestone#region#<WorldName>` (identifier is the exact Lodestone world name, e.g. `Gilgamesh`).
- Physical regions: `na`, `eu`, `jp`, `oce` (from HTML tab panels, not from visible UI tab).
- Discord `ServerLabel` = `ffxiv-<region>-<slug>` (e.g. `ffxiv-na-gilgamesh`).

**Maintainer workflow:** run `generate-bnet-configs` and/or `generate-ffxiv-configs`; the FFXIV script merges into existing `config-out/server-mapping.json` by default. Upload the merged mapping to S3 when ready.

### FFXIV polling Lambda

[`ffxiv-polling-function`](../cmd/ffxiv-polling-function/) runs as **`FFXIVPollingLambda`** via [`internal/ffxivpoller`](../internal/ffxivpoller/). Required env: `CONFIG_BUCKET`, `FFXIV_LODESTONE_CONFIG_PATH` (S3 key for `ffxiv-lodestone-config.json`), `DDB_TABLE_NAME` (same status table as Battle.net). No SSM secrets.

Live status is read from Square Enix **frontier JSON** (`frontier_status_url` in config, or the default launcher feed). If the whole frontier fetch or parse fails, the poller falls back once to the **Lodestone HTML** world status page (`lodestone_url`). Catalog world names must match frontier keys exactly.

Example `games.ffxiv` fragment:

```json
"ffxiv": {
  "provider": "lodestone",
  "regions": {
    "na": { "servers": { "gilgamesh": { "identifier": "Gilgamesh" } } }
  }
}
```

## Future: unified catalog

When additional poller Lambdas exist (API, scraper, pinger), the intended model is:

- **`server-mapping.json`** remains the single catalog of monitored servers.
- Each poller filters entries where `provider` matches its integration and polls only those targets.

Unifying BNet polling with the mapping catalog is deferred until that multi-poller layout is designed.
