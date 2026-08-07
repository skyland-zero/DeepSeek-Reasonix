package cli

import (
	"errors"
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/opencodego"
	"reasonix/internal/testenv"
)

func TestQuotaBar(t *testing.T) {
	cases := []struct {
		percent int
		want    string
	}{
		{0, "░░░░░░░░░░░░"},
		{50, "██████░░░░░░"},
		{100, "████████████"},
		{42, "█████░░░░░░░"},
		{150, "████████████"},
		{-5, "░░░░░░░░░░░░"},
	}
	for _, c := range cases {
		if got := quotaBar(c.percent); got != c.want {
			t.Errorf("quotaBar(%d) = %q, want %q", c.percent, got, c.want)
		}
	}
}

func TestFormatReset(t *testing.T) {
	cases := []struct {
		secs int
		want string
	}{
		{0, "0s"},
		{59, "59s"},
		{60, "1m"},
		{3599, "59m"},
		{3600, "1h0m"},
		{5400, "1h30m"},
		{86399, "23h59m"},
		{86400, "1d0h"},
		{90061, "1d1h"},
	}
	for _, c := range cases {
		if got := formatReset(c.secs); got != c.want {
			t.Errorf("formatReset(%d) = %q, want %q", c.secs, got, c.want)
		}
	}
}

func TestRunQuotaCommandMissingCredentials(t *testing.T) {
	restore, err := testenv.IsolateUserState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	t.Setenv(opencodego.CookieEnv, "")
	t.Setenv(opencodego.WorkspaceEnv, "")

	var commit []string
	m := &chatTUI{pendingCommit: &commit}
	cmd := m.runQuotaCommand("/quota")
	if cmd == nil {
		t.Fatal("runQuotaCommand(/quota) returned nil cmd")
	}
	msg := cmd()
	qd, ok := msg.(quotaDoneMsg)
	if !ok {
		t.Fatalf("msg type = %T, want quotaDoneMsg", msg)
	}
	if qd.err == nil {
		t.Fatal("expected credential error, got nil")
	}
}

func TestRunQuotaCommandSetupUsage(t *testing.T) {
	var commit []string
	m := &chatTUI{pendingCommit: &commit}
	m.runQuotaCommand("/quota setup")
	if len(commit) == 0 {
		t.Fatal("setup printed nothing")
	}
	if joined := strings.Join(commit, "\n"); !strings.Contains(joined, "/quota setup") {
		t.Errorf("setup usage missing: %s", joined)
	}
}

func TestRunQuotaCommandSetupStoresCredentials(t *testing.T) {
	restore, err := testenv.IsolateUserState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)

	var commit []string
	m := &chatTUI{pendingCommit: &commit}
	m.runQuotaCommand("/quota setup wrk_test auth=123")
	if len(commit) == 0 {
		t.Fatal("setup printed nothing")
	}
	got := opencodego.ResolveCredentials()
	if got.WorkspaceID != "wrk_test" || got.Cookie != "auth=123" {
		t.Errorf("credentials not stored: %+v", got)
	}
}

func TestRunQuotaCommandLogoutClearsCredentials(t *testing.T) {
	restore, err := testenv.IsolateUserState()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	if err := opencodego.StoreCredentials("auth=123", "wrk_test"); err != nil {
		t.Fatal(err)
	}

	var commit []string
	m := &chatTUI{pendingCommit: &commit}
	m.runQuotaCommand("/quota logout")
	if got := opencodego.ResolveCredentials(); got.Cookie != "" || got.WorkspaceID != "" {
		t.Errorf("credentials not cleared: %+v", got)
	}
}

func TestUpdateQuotaDoneMsgCommitsLines(t *testing.T) {
	m := newChatTUI(control.New(control.Options{}), "", make(chan event.Event, 1), 80)
	next, _ := m.Update(quotaDoneMsg{lines: []string{"line1", "line2"}})
	m = next.(chatTUI)
	if joined := strings.Join(m.transcript, "\n"); !strings.Contains(joined, "line1") || !strings.Contains(joined, "line2") {
		t.Errorf("quota lines not committed to scrollback: %q", joined)
	}
}

func TestUpdateQuotaDoneMsgErrorNotices(t *testing.T) {
	m := newChatTUI(control.New(control.Options{}), "", make(chan event.Event, 1), 80)
	next, _ := m.Update(quotaDoneMsg{err: errors.New("boom")})
	m = next.(chatTUI)
	if joined := strings.Join(m.transcript, "\n"); !strings.Contains(joined, "boom") {
		t.Errorf("quota error not surfaced: %q", joined)
	}
}
