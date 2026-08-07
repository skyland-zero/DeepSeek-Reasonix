package opencodego

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The RPC payload is SolidJS hydration text (seroval) where each quota window
// appears as rollingUsage:{$R[N]=}{status:"...",resetInSec:N,usagePercent:N}.
// The $R[N] reference number drifts as fields before it are added or removed,
// so it must not be pinned — the optional group accepts any index or none.
var (
	rollingRe = regexp.MustCompile(`rollingUsage:(?:\$R\[\d+\]=)?\{status:"([^"]+)",resetInSec:(\d+),usagePercent:(\d+)\}`)
	weeklyRe  = regexp.MustCompile(`weeklyUsage:(?:\$R\[\d+\]=)?\{status:"([^"]+)",resetInSec:(\d+),usagePercent:(\d+)\}`)
	monthlyRe = regexp.MustCompile(`monthlyUsage:(?:\$R\[\d+\]=)?\{status:"([^"]+)",resetInSec:(\d+),usagePercent:(\d+)\}`)
)

func parseQuota(text string) (*QuotaData, error) {
	rolling := rollingRe.FindStringSubmatch(text)
	weekly := weeklyRe.FindStringSubmatch(text)
	if rolling == nil {
		return nil, fmt.Errorf("opencode go: rollingUsage missing from response")
	}
	if weekly == nil {
		return nil, fmt.Errorf("opencode go: weeklyUsage missing from response")
	}
	data := &QuotaData{Rolling: parseUsage(rolling), Weekly: parseUsage(weekly)}
	if monthly := monthlyRe.FindStringSubmatch(text); monthly != nil {
		if u := parseUsage(monthly); u.Status != "unlimited" {
			data.Monthly = &u
		}
	}
	return data, nil
}

func parseUsage(m []string) QuotaUsage {
	reset, _ := strconv.Atoi(m[2])
	percent, _ := strconv.Atoi(m[3])
	return QuotaUsage{Status: m[1], UsagePercent: percent, ResetInSec: reset}
}

// authRedirect reports whether the RPC payload is a serialized 302 redirect
// to the opencode.ai authorization page — the server's way of rejecting an
// invalid or expired session cookie.
func authRedirect(text string) bool {
	return strings.Contains(text, "/auth/authorize") && strings.Contains(text, "302")
}
