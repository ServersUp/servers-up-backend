# Configuration model

## Server catalog (`server-mapping.json`)

Discord commands (`/subscribe`, `/games`, `/servers`, `/status`, autocomplete, notification labels) use **`server-mapping.json`** in S3 (`CONFIG_BUCKET` / `SERVER_MAPPING_PATH`). Each game has a `provider`, and each server has `region` and `identifier` (for Battle.net, the connected realm ID). `/status` resolves game/server through the mapping, then reads live status from the status DynamoDB table (`DDB_GAME_SERVER_STATUS_TABLE_NAME` on the bot Lambda).

## Battle.net polling config (separate today)

The [`bnet-polling-function`](../cmd/bnet-polling-function/) reads a **separate** S3 JSON (`BNET_SERVER_CONFIG_PATH`) with region, locale, and a `realms[]` list (`connected_realm_id`, etc.). Status is written to the status DynamoDB table.

## Future: unified catalog

When additional poller Lambdas exist (API, scraper, pinger), the intended model is:

- **`server-mapping.json`** remains the single catalog of monitored servers.
- Each poller filters entries where `provider` matches its integration and polls only those targets.

Unifying BNet polling with the mapping catalog is deferred until that multi-poller layout is designed.
