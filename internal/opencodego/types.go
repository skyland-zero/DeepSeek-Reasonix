// Package opencodego queries OpenCode Go subscription quota from opencode.ai.
//
// OpenCode Go does not expose a public usage API; the readout is fetched from
// the site's internal server-function RPC endpoint and parsed out of the
// SolidJS hydration (seroval) payload. Credentials are an opencode.ai session
// cookie plus the workspace id, stored through the shared skycode credential
// store so one setup serves every project.
package opencodego

import "time"

// QuotaUsage is one quota window's readout. The RPC payload only carries a
// usage percentage plus the seconds until the window resets — there is no
// absolute limit or remaining figure.
type QuotaUsage struct {
	Status       string // "ok", "unlimited", ...
	UsagePercent int    // 0-100
	ResetInSec   int    // seconds until the window resets
}

// QuotaData is the full quota readout for an OpenCode Go workspace.
type QuotaData struct {
	Rolling   QuotaUsage  // ~5-hour rolling window
	Weekly    QuotaUsage  // weekly window
	Monthly   *QuotaUsage // monthly window; nil when unlimited
	FetchedAt time.Time
}
