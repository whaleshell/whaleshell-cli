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

const rootHelp = `osg — OpenShell-compatible agent sandbox CLI

Usage:
  osg [global flags] <command> [flags]

Global flags:
  -g, --gateway NAME     Select gateway (or OPENSHELL_GATEWAY / OSG_GATEWAY)
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

Run 'osg <command> --help' for details.
`

var helpTree = map[string]string{
	"sandbox": `osg sandbox — manage sandboxes

Usage:
  osg sandbox create|list|get|stop|start|delete|exec|connect|upload|download|ssh-config|provider|template …

Aliases: sb

Examples:
  osg sandbox create --name app --from ollama
  osg sandbox list
  osg sandbox exec app -- ls /
`,
	"sandbox create": `osg sandbox create — create a sandbox

Usage:
  osg sandbox create --name NAME [flags]

Flags (OpenShell-aligned):
  --name NAME
  --from base|ollama|cursor|claude|…
  --image IMAGE
  --policy PATH
  --cpu N --memory SIZE
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
  --agent-prompt PATH     → /etc/osg/agent-payload/agent-prompt.md
  --cursor-cli-config PATH → $HOME/.cursor/cli-config.json (attribution off by default)
  --no-agent-config       skip builtin /etc/osg inject
`,
	"sandbox template": `osg sandbox template — workload templates

Usage:
  osg sandbox template create|list|get|delete …
`,
	"sandbox provider": `osg sandbox provider — attach providers to a sandbox

Usage:
  osg sandbox provider list|attach|detach …
`,
	"provider": `osg provider — provider instances

Usage:
  osg provider create|list|get|update|delete|profile|refresh|effective …

Examples:
  osg provider create --name gh --type github --from-existing
  osg provider refresh configure NAME --credential-key K --strategy oauth2-refresh-token
`,
	"provider profile": `osg provider profile — custom provider YAML profiles

Usage:
  osg provider profile list|show|import|export|delete|lint …
`,
	"provider refresh": `osg provider refresh — credential refresh strategies

Usage:
  osg provider refresh status|configure|rotate|delete …

Strategies: env | oauth2-refresh-token | oauth2-client-credentials | aws-sts-assume-role
`,
	"policy": `osg policy — network policy

Usage:
  osg policy get|set|update|list|delete|check …
`,
	"gateway": `osg gateway — manage gateways

Usage:
  osg gateway add|remove|select|info|list|login|logout
`,
	"workspace": `osg workspace — workspaces (gateway-backed)

Usage:
  osg workspace create --name NAME
  osg workspace list|get|delete NAME
  osg workspace member add|remove|list …
`,
	"workspace member": `osg workspace member — manage members

Usage:
  osg workspace member add --workspace NAME --subject SUBJECT [--role user|admin]
  osg workspace member remove --workspace NAME --subject SUBJECT
  osg workspace member list --workspace NAME
`,
	"service": `osg service — expose HTTP services

Usage:
  osg service expose <sandbox> <port> [name]
  osg service list|get|delete …

Edge URL (gateway Host router):
  http://<name>.openshell.localhost:<gateway-port>/
`,
	"forward": `osg forward — TCP forwards into a sandbox

Usage:
  osg forward start <host-port> <sandbox> [-d]
  osg forward stop <id>
  osg forward list
`,
	"inference": `osg inference — inference.local routing

Usage:
  osg inference get|set|update|list|show|local
`,
	"settings": `osg settings — key/value settings

Usage:
  osg settings get|set|delete …
`,
	"logs": `osg logs — sandbox logs

Usage:
  osg logs <name> [--tail] [-n N] [--since 5m]
`,
	"doctor": `osg doctor — environment checks

Usage:
  osg doctor [check]
`,
	"install": `osg install — install CLI binary and ensure local gateway

Usage:
  osg install [--force]

Copies osg to ~/.local/share/osg/bin, symlinks ~/.local/bin/osg,
then starts/selects a local osg-gateway if needed.
`,
	"whoami": `osg whoami — print identity

Usage:
  osg whoami
  osg -o json whoami
`,
	"completions": `osg completions — shell completions

Usage:
  osg completions <bash|zsh|fish|powershell>
`,
	"status": `osg status — gateway connectivity

Usage:
  osg status
  osg -o json status
`,
	"term": `osg term — interactive TUI

Usage:
  osg term
`,
	"rule": `osg rule — approval rules (MVP)

Usage:
  osg rule get|approve|reject|history|clear …
`,
}
