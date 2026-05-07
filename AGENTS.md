# ServersUp backend — agent context

## Repo map (quick navigation)

- **Entry points**
  - `README.md`: architecture overview and high-level directory layout (may lag `cmd/`; trust tree + this file).
  - `AGENTS.md` (this file): repo map, Go/CI conventions, append-only agent memory log.
  - `.cursor/rules/agents-md.mdc`: Cursor rule that keeps this map and log expectations aligned with the codebase.
  - `go.mod` / `go.sum`: module path `github.com/ServersUp/servers-up-backend`, Go toolchain version.
  - `.github/workflows/lambda_deployment.yaml`: OIDC deploy pipeline (discover Lambdas, test, build, multi-region `update-function-code`).

- **`cmd/` — binaries and deployable Lambdas**
  - **`config-reader/`**: CLI used only in CI; reads `deployment-config.yaml` and emits JSON matrix rows (`go run ./cmd/config-reader/main.go <yaml> <lambda-id>`). No `deployment-config.yaml` here.
  - **`bnet-polling-function/`**: Scheduled/event Lambda; Battle.net polling, writes `GameServerStatus`-style rows via `internal/db` (`DDB_TABLE_NAME`), reads SSM + S3 config via `internal/config`.
  - **`discord-bot-api/`**: Lambda Function URL handler; Ed25519-verified Discord interactions (`internal/discord`). Slash commands: `subscribe` (game/server autocomplete + optional role), `unsubscribe` (guild-wide `subscription` autocomplete), `subscriptions`, `help`. Reads server mapping JSON from S3 (`CONFIG_BUCKET` / `SERVER_MAPPING_PATH`), subscriptions table (`DDB_SUBSCRIPTIONS_TABLE_NAME`), Discord public key from SSM (`DISCORD_BOT_PUBLIC_KEY_PATH`).
  - **`discord-guild-notify-job-creator/`**: Lambda (invoked from DynamoDB stream in infra); enqueues guild notify jobs to SQS; uses `internal/config`, subscription reads, etc. See stack wiring in Terraform repo.
  - **`discord-guild-notify-lambda/`**: SQS-triggered Lambda; sends Discord notifications; tests + deployment config alongside `main.go`.

  Each Lambda that ships through CI has **`cmd/<name>/deployment-config.yaml`** with `type: lambda` and a `regions:` list consumed by `config-reader`.

- **`internal/` — shared libraries**
  - **`bnet/`**: Battle.net API client + models + tests.
  - **`config/`**: S3 + SSM provider (`NewProvider`, secrets, `LoadJSONFromS3`).
  - **`db/`**: DynamoDB: game server status upserts (`SaveServerStatus` with read-before-write), subscriptions (`AddSubscription`, `ListSubscriptionsByGuild`, `DeleteSubscription`, `ListSubscriptionsByServer`), `GuildIdIndex` for guild queries.
  - **`discord/`**: Interaction types, `VerifySignature`, request/response models (including autocomplete).
  - **`models/`**: Shared structs (`GameServerStatus`, `Subscription`, `GuildNotifyJob`).
  - **`servermap/`**: `server-mapping.json` shape, `Lookup`, `ListGames` / `ListServers`, `NormalizeKey`.
  - **`serverid/`**: Provider/region/identifier → canonical `serverId` string.

- **CI / deploy mental model**
  - **Discover**: directories under `cmd/*/` that contain `deployment-config.yaml` with `type: lambda`.
  - **Test + build**: matrix per discovered Lambda; `go test -v ./...` (whole module each time), then `go build` of that Lambda’s `main.go` → `bootstrap` → zip artifact per matrix cell.
  - **Deploy**: `get-config` merges YAML-driven region rows; `deploy-multi-region` assumes GitHub `vars` like `AWS_ROLE_US_EAST_1` (pattern `AWS_ROLE_<REGION>`), downloads matching artifact, `aws lambda update-function-code`.
  - **Triggers**: `push` to `main` and `workflow_dispatch`. There is **no PR-only plan job** in this workflow—behavior changes land via review then merge.

- **Conventions used here**
  - **Imports**: standard library first, then third-party, then `github.com/ServersUp/servers-up-backend/internal/...`.
  - **Logging**: `log/slog` (JSON in Lambda).
  - **Tests**: `*_test.go` next to code; Discord handler tests use ed25519 signing (`cmd/discord-bot-api/main_test.go`).
  - **New Lambda**: add `cmd/<service>/main.go`, `deployment-config.yaml` with `type: lambda`, ensure workflow discovers it; infra side must create function + IAM/env (separate Terraform repo).

---

## Working in this repo (fast paths)

- **Tests**
  - `go test ./...`
- **Build one Lambda binary (linux/amd64, matches CI roughly)**
  - `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bootstrap ./cmd/discord-bot-api/main.go`
- **Config reader (local)**
  - `go run ./cmd/config-reader/main.go ./cmd/discord-bot-api/deployment-config.yaml discord-bot-api`

---

## Git / GitHub (no assistant attribution)

- Do **not** put generic “AI generated” lines, IDE/agent watermarks, footers, or branding on **PR titles**, **PR bodies**, **commit messages**, or other public GitHub text.
- Do **not** add **co-author**, **co-commit**, `Co-authored-by:`, or signatures that attribute work to a coding assistant or tool—keep commits and PR metadata indistinguishable from a normal contributor.
- **Cursor** can **append** a “Made with …” PR footer or commit trailers **after** automation—disable in **Cursor Settings → Agents → Attribution** (PR + commit attribution), restart Cursor; for CLI, use `~/.cursor/cli-config.json` with `"prAttribution": false` and `"commitAttribution": false` when documented.

---

## Agent memory log (token-tight)

### Format (append-only)

Each entry is one line, commit-sized, optimized for future prompts.

Template:

- **YYYY-MM-DD** `(<scope>) <what changed> — <why it matters> [files: a,b,c]`

Guidelines:

- One line, no filler; **scope** examples: `cmd/discord-bot-api`, `internal/db`, `ci`, `discord`.
- Prefer **behavioral** impact over implementation detail; add **files:** only when it disambiguates.
- **PR titles**: Title Case.
- Do not rewrite or delete prior log lines.

### Log

- **2026-05-07** `(docs) Add AGENTS.md and Cursor agents-md rule — maps cmd/internal + lambda_deployment workflow for agent navigation [files: AGENTS.md, .cursor/rules/agents-md.mdc]`

---

## Optional: local-only copy (infra-repo style)

Some teams **gitignore** `AGENTS.md` and `.cursor/` and keep them only on disk. This repo **tracks** them by default so the map is shared; to switch, add those paths to `.gitignore` and maintain a private copy.
