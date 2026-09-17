// Package global parses OpenShell-style CLI globals once before subcommands.
package global

import (
	"os"
	"strings"
)

// Context is the resolved CLI session context.
type Context struct {
	GatewayName string // -g / --gateway (name)
	GatewayURL  string // resolved endpoint override
	Workspace   string // --workspace / OPENSHELL_WORKSPACE
	Output      string // -o / --output: json|yaml|text
}

// Parse strips leading global flags from args and returns the remainder.
//
// Supported globals (anywhere before the first non-flag positional command group
// is handled by stripping all leading dash-args that match known globals):
//
//	-g/--gateway NAME
//	--workspace NAME
//	-o/--output json|yaml|text
//
// Env: OPENSHELL_GATEWAY (name or URL), OSG_GATEWAY_URL, OPENSHELL_WORKSPACE, OSG_WORKSPACE.
func Parse(args []string) (Context, []string) {
	ctx := Context{
		Workspace: firstNonEmpty(os.Getenv("OPENSHELL_WORKSPACE"), os.Getenv("OSG_WORKSPACE"), "default"),
		Output:    "text",
	}
	if v := firstNonEmpty(os.Getenv("OPENSHELL_GATEWAY"), os.Getenv("OSG_GATEWAY_URL")); v != "" {
		if looksLikeURL(v) {
			ctx.GatewayURL = v
		} else {
			ctx.GatewayName = v
		}
	}

	out := make([]string, 0, len(args))
	i := 0
	// Only peel globals from the front so `sandbox create --workspace DIR` keeps its flag.
	for i < len(args) {
		a := args[i]
		switch a {
		case "-g", "--gateway":
			i++
			if i < len(args) {
				v := args[i]
				if looksLikeURL(v) {
					ctx.GatewayURL = v
					ctx.GatewayName = ""
				} else {
					ctx.GatewayName = v
				}
				i++
			}
		case "--workspace":
			i++
			if i < len(args) {
				ctx.Workspace = args[i]
				i++
			}
		case "-o", "--output":
			i++
			if i < len(args) {
				ctx.Output = strings.ToLower(args[i])
				i++
			}
		default:
			if strings.HasPrefix(a, "-g=") {
				v := strings.TrimPrefix(a, "-g=")
				if looksLikeURL(v) {
					ctx.GatewayURL = v
				} else {
					ctx.GatewayName = v
				}
				i++
				continue
			}
			if strings.HasPrefix(a, "--gateway=") {
				v := strings.TrimPrefix(a, "--gateway=")
				if looksLikeURL(v) {
					ctx.GatewayURL = v
				} else {
					ctx.GatewayName = v
				}
				i++
				continue
			}
			if strings.HasPrefix(a, "--workspace=") {
				ctx.Workspace = strings.TrimPrefix(a, "--workspace=")
				i++
				continue
			}
			if strings.HasPrefix(a, "-o=") {
				ctx.Output = strings.ToLower(strings.TrimPrefix(a, "-o="))
				i++
				continue
			}
			if strings.HasPrefix(a, "--output=") {
				ctx.Output = strings.ToLower(strings.TrimPrefix(a, "--output="))
				i++
				continue
			}
			out = append(out, args[i:]...)
			return ctx, out
		}
	}
	return ctx, out
}

func looksLikeURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.Contains(s, "://") || strings.HasPrefix(s, "127.") || strings.HasPrefix(s, "localhost")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
