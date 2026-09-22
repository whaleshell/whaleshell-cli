// Package domain holds CLI-facing entities without I/O.
package domain

// SandboxRef identifies a local or gateway-managed sandbox.
type SandboxRef struct {
	Name   string
	Status string
}

// GatewaySession is the selected control-plane endpoint + auth material refs.
type GatewaySession struct {
	Name string
	URL  string
}

// PolicyDocPath is a filesystem path to a policy YAML document.
type PolicyDocPath string
