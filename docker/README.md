# whaleshell-cli/docker/

Agent sandbox images for the `whaleshell` CLI (`--from cursor|claude|codex`).  
Runtime base (`whaleshell-sandbox:local` / GHCR `:cli`) is built from [`whaleshell-runtime/images/sandbox`](../../whaleshell-runtime/images/sandbox/) via `task runtime:image:cli`.

| Path | Tag | Contents |
|------|-----|----------|
| [`agents/cursor`](./agents/cursor/) | `whaleshell-sandbox:cursor` | + Cursor Agent CLI under `/opt/cursor-agent` |
| [`agents/claude`](./agents/claude/) | `whaleshell-sandbox:claude` | + Claude Code CLI |
| [`agents/codex`](./agents/codex/) | `whaleshell-sandbox:codex` | + OpenAI Codex CLI |

Published images and BYOC: [docs/IMAGES.md](../../docs/IMAGES.md).

## Build

```bash
export GOWORK=$PWD/go.work
task runtime:image:cli          # base → whaleshell-sandbox:local
task docker:agent:cursor        # → whaleshell-sandbox:cursor
task docker:agent:claude
task docker:agent:codex
task docker:agent:all
```

## Use

```bash
whaleshell sandbox create --name cursor \
  --from cursor \
  --workspace . \
  --policy whaleshell-cli/policies/cursor.yaml \
  -- agent
```
