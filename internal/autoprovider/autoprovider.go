// Package autoprovider merges explicit --provider flags with trailing-command inference.
package autoprovider

import "strings"

// Mode controls OpenShell-style auto provider discovery from argv.
type Mode int

const (
	// ModeOff never infers (Providers v2 default).
	ModeOff Mode = iota
	// ModeOn infers from known agent commands.
	ModeOn
)

// Merge combines explicit providers with optional inference.
// Explicit names win for ordering; inferred names are appended if missing.
func Merge(explicit, inferred []string, mode Mode) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(list []string) {
		for _, p := range list {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	add(explicit)
	if mode == ModeOn {
		add(inferred)
	}
	return out
}
