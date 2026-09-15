# osg-cli/docker/

Agent sandbox images for the `osg` CLI (`--from cursor|claude|codex`).  
Runtime base image (`osg-sandbox:local`) is built from [`osg-runtime/images/sandbox`](../../osg-runtime/images/sandbox/) via `task runtime:image:cli`.

| Path | Tag | Contents |
|------|-----|----------|
| [`agents/base`](./agents/base/) | `osg-sandbox:local` | Debian + curl + git + osg-init (same as runtime cli target) |
| [`agents/cursor`](./agents/cursor/) | `osg-sandbox:cursor` | + Cursor Agent CLI under `/opt/cursor-agent` (`agent` → bundled `node`) |
| [`agents/claude`](./agents/claude/) | `osg-sandbox:claude` | + Claude Code CLI |
| [`agents/codex`](./agents/codex/) | `osg-sandbox:codex` | + OpenAI Codex CLI |

## Build

From org root (needs Docker + Go):

```bash
export GOWORK=$PWD/go.work
task runtime:image:cli          # base → osg-sandbox:local
task docker:agent:cursor        # → osg-sandbox:cursor
task docker:agent:claude
task docker:agent:codex
task docker:agent:all
```

Or directly:

```bash
docker build -t osg-sandbox:cursor \
  -f osg-cli/docker/agents/cursor/Dockerfile \
  osg-cli/docker/agents/cursor
```

## Use

```bash
go build -C osg-cli -o osg ./cmd/osg
./osg install   # optional: ~/.local/bin/osg

osg sandbox create --name cursor \
  --from cursor \
  --workspace . \
  --policy osg-cli/policies/cursor.yaml \
  -- agent
```

Interactive TUI needs a real host TTY (`osg connect` / `osg exec` without `--no-tty`).  
Workspace trust prompt: press `a` or Enter after the TTY fix; for headless use `agent -p --trust "…"`.
