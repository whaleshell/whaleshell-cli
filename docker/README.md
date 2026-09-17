# osg-cli/docker/

Agent sandbox images for the `osg` CLI (`--from cursor|claude|codex`).  
Runtime base (`osg-sandbox:local` / GHCR `:cli`) is built from [`osg-runtime/images/sandbox`](../../osg-runtime/images/sandbox/) via `task runtime:image:cli`.

| Path | Tag | Contents |
|------|-----|----------|
| [`agents/cursor`](./agents/cursor/) | `osg-sandbox:cursor` | + Cursor Agent CLI under `/opt/cursor-agent` |
| [`agents/claude`](./agents/claude/) | `osg-sandbox:claude` | + Claude Code CLI |
| [`agents/codex`](./agents/codex/) | `osg-sandbox:codex` | + OpenAI Codex CLI |

Published images and BYOC: [docs/IMAGES.md](../../docs/IMAGES.md).

## Build

```bash
export GOWORK=$PWD/go.work
task runtime:image:cli          # base → osg-sandbox:local
task docker:agent:cursor        # → osg-sandbox:cursor
task docker:agent:claude
task docker:agent:codex
task docker:agent:all
```

## Use

```bash
osg sandbox create --name cursor \
  --from cursor \
  --workspace . \
  --policy osg-cli/policies/cursor.yaml \
  -- agent
```
