<h1 align="center">osg-cli</h1>

<p align="center">
  <strong>Agent sandbox CLI</strong><br>
  Create, harden, and operate policy-bound sandboxes — Cursor, Claude, Codex, and BYOC.
</p>
<p align="center">
  <a href="https://github.com/zorneth/osg-cli/actions/workflows/ci.yml"><img src="https://github.com/zorneth/osg-cli/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/zorneth/osg-cli"><img src="https://pkg.go.dev/badge/github.com/zorneth/osg-cli.svg" alt="Go Reference"></a>
  <a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License"></a>
  <a href="https://github.com/zorneth/osg-cli"><img src="https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go" alt="Go Version"></a>
</p>
<p align="center">
  <sub>Part of the <a href="https://github.com/zorneth">zorneth / osg</a> ecosystem</sub>
</p>

---

## Overview

**osg-cli** is the user-facing `osg` binary for the osg ecosystem: sandbox lifecycle, policy checks, provider attach, gateway selection, live logs, and agent images.

### Key Features

| Category | Capabilities |
|----------|--------------|
| **Sandboxes** | create / exec / connect / rm — Docker-first via `osg-driver` |
| **Policy** | YAML check/compose with `osg-core` + provider presets |
| **Agents** | `--from cursor\|claude\|codex\|base\|gui\|gpu` (GHCR or local) |
| **Providers** | Cursor, GitHub, NVIDIA, local inference (`host.osg.internal`) |
| **Observability** | `osg logs --tail`, TUI (`osg term`), OCSF audit lines |
| **Gateway** | register with `osg-gateway`, relay exec, policy proposals |

---

## Installation

```bash
go install github.com/zorneth/osg-cli/cmd/osg@latest
# or from a workspace checkout:
go build -C . -o osg ./cmd/osg
./osg install   # ~/.local/bin/osg
```

**Requirements:** Go 1.27+, Docker or Podman.

---

## Quick Start

```bash
./osg status
./osg policy check ./policies/default.yaml

./osg sandbox create --name demo --workspace . --policy ./policies/default.yaml
./osg sandbox exec demo -- uname -a
./osg sandbox rm demo
```

### Cursor agent

```bash
./osg sandbox create --name cursor --from cursor --workspace "$PWD" \
  --provider cursor --provider gh --policy ./policies/cursor.yaml
./osg sandbox connect cursor -- agent
```

Agent Dockerfiles live in [`docker/agents/`](./docker/agents/). Policies: [`policies/`](./policies/).

---

## Package Structure

| Path | Purpose |
|------|---------|
| `cmd/osg` | CLI entrypoint |
| `internal/app` | Commands (sandbox, proxy, gateway, rules, …) |
| `policies/` | Builtin policy YAML |
| `docker/agents/` | cursor / claude / codex images |


---

## Related

| Resource | Link |
|----------|------|
| Roadmap | [ROADMAP.md](./ROADMAP.md) |
| Organization | [https://github.com/zorneth](https://github.com/zorneth) |
| Organization overview | [github.com/zorneth](https://github.com/zorneth) |
| pkg.go.dev | [`github.com/zorneth/osg-cli`](https://pkg.go.dev/github.com/zorneth/osg-cli) |

## License

[MIT](./LICENSE) © zorneth
