// Package branding centralizes the fork's user-visible identity.
//
// Merge contract: config file names, environment variables, path conventions,
// docs, comments and tests keep the upstream "reasonix" naming so upstream
// merges stay small. Only these display identities are customized; every
// runtime surface must read them from this package instead of hardcoding a
// product name.
package branding

const (
	// ProductName is the human-facing product name used in banners, welcome
	// text, the system prompt identity, and user-facing prose.
	ProductName = "Skycode"

	// BinaryName is the CLI binary/command name users type.
	BinaryName = "skycode"

	// AgentName is the identity reported to ACP peers.
	AgentName = "skycode"
)
