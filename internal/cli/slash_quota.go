package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/i18n"
	"reasonix/internal/netclient"
	"reasonix/internal/opencodego"
)

// quotaDoneMsg carries the result of an async /quota readout. lines is nil
// when the query failed; err explains why.
type quotaDoneMsg struct {
	lines []string
	err   error
}

// runQuotaCommand handles "/quota" and its "/usage" alias. The readout runs
// off the Update loop so the TUI stays responsive; setup/logout are
// synchronous credential-store operations.
func (m *chatTUI) runQuotaCommand(input string) tea.Cmd {
	typed := strings.TrimSpace(strings.SplitN(input, " ", 2)[0])
	args := strings.TrimSpace(strings.TrimPrefix(input, typed))
	if fields := strings.Fields(args); len(fields) > 0 {
		switch fields[0] {
		case "setup":
			m.runQuotaSetup(strings.TrimSpace(strings.TrimPrefix(args, fields[0])))
			return nil
		case "logout":
			if err := opencodego.ClearCredentials(); err != nil {
				m.notice(fmt.Sprintf("%s: %v", i18n.M.QuotaLogoutFailed, err))
				return nil
			}
			m.notice(i18n.M.QuotaLogoutDone)
			return nil
		}
	}
	return func() tea.Msg {
		lines, err := quotaReadout()
		return quotaDoneMsg{lines: lines, err: err}
	}
}

// quotaReadout fetches and formats the OpenCode Go quota outside the TUI
// event loop.
func quotaReadout() ([]string, error) {
	creds := opencodego.ResolveCredentials()
	client, err := netclient.NewHTTPClient(netclient.ProxySpec{Mode: netclient.ModeAuto}, netclient.TransportOptions{
		DialTimeout:           10 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	client.Timeout = 30 * time.Second
	data, err := opencodego.FetchQuota(context.Background(), client, creds.Cookie, creds.WorkspaceID)
	if err != nil {
		return nil, err
	}
	lines := []string{viewHeader("OpenCode Go quota")}
	lines = append(lines, quotaLine("rolling", &data.Rolling))
	lines = append(lines, quotaLine("weekly", &data.Weekly))
	lines = append(lines, quotaLine("monthly", data.Monthly))
	lines = append(lines, viewMeta(fmt.Sprintf("  fetched   %s", data.FetchedAt.Local().Format("15:04:05"))))
	return lines, nil
}

func quotaLine(label string, u *opencodego.QuotaUsage) string {
	if u == nil {
		return fmt.Sprintf("  %-9s unlimited", label)
	}
	return fmt.Sprintf("  %-9s %s %3d%% · reset in %s",
		label, quotaBar(u.UsagePercent), u.UsagePercent, formatReset(u.ResetInSec))
}

// quotaBar renders a 12-cell progress bar for a 0-100 usage percentage.
func quotaBar(percent int) string {
	const width = 12
	filled := percent * width / 100
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// formatReset renders a seconds countdown compactly, e.g. 3h24m.
func formatReset(secs int) string {
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh%dm", secs/3600, secs%3600/60)
	default:
		return fmt.Sprintf("%dd%dh", secs/86400, secs%86400/3600)
	}
}

// runQuotaSetup stores the OpenCode Go session cookie and workspace id in the
// shared credential store, or prints how to obtain them.
func (m *chatTUI) runQuotaSetup(args string) {
	lines := []string{
		viewHeader("OpenCode Go quota setup"),
		viewHint("usage: /quota setup <workspace-id> <auth-cookie>"),
		viewHint("1. open https://opencode.ai/auth and sign in"),
		viewHint("2. copy the workspace id (wrk_...) from the page"),
		viewHint("3. copy the auth cookie from browser DevTools (Application > Cookies)"),
		viewHint("or set " + opencodego.CookieEnv + " and " + opencodego.WorkspaceEnv + " env vars instead"),
	}
	parts := strings.Fields(args)
	if len(parts) != 2 {
		m.commitLine(strings.Join(lines, "\n"))
		return
	}
	if err := opencodego.StoreCredentials(parts[1], parts[0]); err != nil {
		m.notice(fmt.Sprintf("%s: %v", i18n.M.QuotaSetupFailed, err))
		return
	}
	m.notice(i18n.M.QuotaSetupDone)
}
