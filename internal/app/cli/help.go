package cli

import (
	"fmt"
	"strings"
)

// wantsHelp reports whether argv asks for help (-h/--help/help).
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "--help", "help":
			return true
		}
	}
	return false
}

// helpPath strips help flags and returns the command path for nested help.
func helpPath(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "-h", "--help", "help":
			continue
		default:
			out = append(out, a)
		}
	}
	return out
}

func printHelp(args []string) error {
	path := helpPath(args)
	fmt.Print(helpText(path))
	return nil
}

func helpText(path []string) string {
	if len(path) == 0 {
		return rootHelp
	}
	key := strings.Join(path, " ")
	if t, ok := helpTree[key]; ok {
		return t
	}
	for i := len(path) - 1; i >= 1; i-- {
		parent := strings.Join(path[:i], " ")
		if t, ok := helpTree[parent]; ok {
			return t
		}
	}
	if t, ok := helpTree[path[0]]; ok {
		return t
	}
	return rootHelp
}

const rootHelp = `whaleshell — OpenShell-compatible agent sandbox CLI

Usage:
  whaleshell [global flags] <command> [flags]

Global flags:
  -g, --gateway NAME     Select gateway (or OPENSHELL_GATEWAY / WHALESHELL_GATEWAY)
  --workspace NAME       Default workspace (or OPENSHELL_WORKSPACE)
  -o, --output FORMAT    text|json|yaml

Commands:
  sandbox (sb)       Create and manage sandboxes
  provider           Provider instances and profiles
  policy (pol)       Network policy get/set/update
  gateway (gw)       Add/select/login gateways
  workspace (ws)     Workspaces and members
  service (svc)      Expose sandbox ports via *.openshell.localhost
  forward (fwd)      TCP port forwards
  inference          Route inference.local
  settings           Gateway/local settings
  logs (lg)          Sandbox logs
  status             Gateway connectivity
  doctor (dr)        Environment checks
  whoami             Identity (supports -o json)
  term               Interactive TUI
  install            Install CLI + ensure local gateway
  completions        Shell completions
  version            Print version

Run 'whaleshell <command> --help' for details.
`

var helpTree = map[string]string{
	"sandbox": `whaleshell sandbox — manage sandboxes

Usage:
  whaleshell sandbox create|list|get|stop|start|delete|exec|connect|upload|download|ssh-config|provider|template …

Aliases: sb

Examples:
  whaleshell sandbox create --name app --from ollama
  whaleshell sandbox list
  whaleshell sandbox exec app -- ls /
`,
	"sandbox create": `whaleshell sandbox create — create a sandbox

Usage:
  whaleshell sandbox create --name NAME [flags]

Flags (OpenShell-aligned):
  --name NAME
  --from base|ollama|cursor|claude|…
  --image IMAGE
  --policy PATH
  --cpu N --memory SIZE   (or set defaults.memory in config / WHALESHELL_DEFAULT_MEMORY)
  --pids-limit N          (-1 unlimited; default 2048 via driver)
  --provider NAME (repeatable)
  --forward PORT (repeatable)
  --workspace PATH
  --label KEY=VALUE
  --driver-config-json JSON
  --agent-config PATH     inject skills/MCP from agent-config.yaml
  --skills PATH           extra skill dir or SKILL.md (repeatable)
  --mcp-cursor PATH       → $HOME/.cursor/mcp.json
  --mcp-claude PATH       → $HOME/.claude/mcp.json
  --harness cursor|claude supervisor harness (default cursor)
  --runtime-mode once|watch
  --agent-prompt PATH     → /etc/whaleshell/agent-payload/agent-prompt.md
  --cursor-cli-config PATH → $HOME/.cursor/cli-config.json (attribution off by default)
  --no-agent-config       skip builtin /etc/whaleshell inject
`,
	"sandbox template": `whaleshell sandbox template — workload templates

Usage:
  whaleshell sandbox template create|list|get|delete …
`,
	"sandbox provider": `whaleshell sandbox provider — attach providers to a sandbox

Usage:
  whaleshell sandbox provider list|attach|detach …
`,
	"provider": `whaleshell provider — provider instances

Usage:
  whaleshell provider create|list|get|update|delete|profile|refresh|effective …

Examples:
  whaleshell provider create --name gh --type github --from-existing
  whaleshell provider refresh configure NAME --credential-key K --strategy oauth2-refresh-token
`,
	"provider profile": `whaleshell provider profile — custom provider YAML profiles

Usage:
  whaleshell provider profile list|show|import|export|delete|lint …
`,
	"provider refresh": `whaleshell provider refresh — credential refresh strategies

Usage:
  whaleshell provider refresh status|configure|rotate|delete …

Strategies: env | oauth2-refresh-token | oauth2-client-credentials | aws-sts-assume-role
`,
	"policy": `whaleshell policy — network policy

Usage:
  whaleshell policy get|set|update|list|delete|check …
`,
	"gateway": `whaleshell gateway — manage gateways

Usage:
  whaleshell gateway ensure|add|remove|select|info|list|login|logout

  ensure   start/select local gateway on 127.0.0.1:7443 if needed
`,
	"workspace": `whaleshell workspace — workspaces (gateway-backed)

Usage:
  whaleshell workspace create --name NAME
  whaleshell workspace list|get|delete NAME
  whaleshell workspace member add|remove|list …
`,
	"workspace member": `whaleshell workspace member — manage members

Usage:
  whaleshell workspace member add --workspace NAME --subject SUBJECT [--role user|admin]
  whaleshell workspace member remove --workspace NAME --subject SUBJECT
  whaleshell workspace member list --workspace NAME
`,
	"service": `whaleshell service — expose HTTP services

Usage:
  whaleshell service expose <sandbox> <port> [name]
  whaleshell service list|get|delete …

Edge URL (gateway Host router):
  http://<name>.openshell.localhost:<gateway-port>/
`,
	"forward": `whaleshell forward — TCP forwards into a sandbox

Usage:
  whaleshell forward start <host-port> <sandbox> [-d]
  whaleshell forward stop <id>
  whaleshell forward list
`,
	"inference": `whaleshell inference — inference.local routing

Usage:
  whaleshell inference get|set|update|list|show|local
`,
	"settings": `whaleshell settings — key/value settings

Usage:
  whaleshell settings get|set|delete …
`,
	"logs": `whaleshell logs — sandbox logs

Usage:
  whaleshell logs <name> [--tail] [-n N] [--since 5m]
`,
	"doctor": `whaleshell doctor — environment checks

Usage:
  whaleshell doctor [check]
`,
	"install": `whaleshell install — install CLI binary and ensure local gateway

Usage:
  whaleshell install [--force]

Copies whaleshell to ~/.local/share/whaleshell/bin, symlinks ~/.local/bin/whaleshell,
then starts/selects a local whaleshell-gateway if needed.
`,
	"whoami": `whaleshell whoami — print identity

Usage:
  whaleshell whoami
  whaleshell -o json whoami
`,
	"completions": `whaleshell completions — shell completions

Usage:
  whaleshell completions <bash|zsh|fish|powershell>
`,
	"status": `whaleshell status — gateway connectivity

Usage:
  whaleshell status
  whaleshell -o json status
`,
	"term": `whaleshell term — interactive TUI

Usage:
  whaleshell term
`,
	"rule": `whaleshell rule — approval rules (MVP)

Usage:
  whaleshell rule get|approve|reject|history|clear …
`,
}
