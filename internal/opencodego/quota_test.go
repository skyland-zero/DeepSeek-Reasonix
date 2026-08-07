package opencodego

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// samplePayload mirrors the SolidJS hydration (seroval) shape returned by the
// opencode.ai RPC endpoint, with a mixture of $R[N] references and inline
// objects to exercise the parser's tolerance.
const samplePayload = `$R[0]="ok";` +
	`$R[1]={status:"ok",resetInSec:3600,usagePercent:10};` +
	`$R[2]={name:"QuotaData",rollingUsage:$R[1]=` +
	`{status:"ok",resetInSec:86400,usagePercent:30},` +
	`weeklyUsage:{status:"ok",resetInSec:300,usagePercent:55},` +
	`monthlyUsage:$R[3]={status:"ok",resetInSec:600,usagePercent:80}}`

func TestParseQuota(t *testing.T) {
	data, err := parseQuota(samplePayload)
	if err != nil {
		t.Fatalf("parseQuota: %v", err)
	}
	if got := data.Rolling.Status; got != "ok" {
		t.Errorf("rolling status = %q, want ok", got)
	}
	if got := data.Rolling.UsagePercent; got != 30 {
		t.Errorf("rolling usagePercent = %d, want 30", got)
	}
	if got := data.Rolling.ResetInSec; got != 86400 {
		t.Errorf("rolling resetInSec = %d, want 86400", got)
	}
	if got := data.Weekly.UsagePercent; got != 55 {
		t.Errorf("weekly usagePercent = %d, want 55", got)
	}
	if data.Monthly == nil {
		t.Fatal("monthly = nil, want a window")
	}
	if got := data.Monthly.UsagePercent; got != 80 {
		t.Errorf("monthly usagePercent = %d, want 80", got)
	}
}

func TestParseQuotaUnlimitedMonthly(t *testing.T) {
	payload := strings.Replace(samplePayload, `monthlyUsage:$R[3]={status:"ok",resetInSec:600,usagePercent:80}`, `monthlyUsage:$R[3]={status:"unlimited",resetInSec:0,usagePercent:0}`, 1)
	data, err := parseQuota(payload)
	if err != nil {
		t.Fatalf("parseQuota: %v", err)
	}
	if data.Monthly != nil {
		t.Errorf("monthly = %+v, want nil for unlimited", data.Monthly)
	}
}

func TestParseQuotaNoReferences(t *testing.T) {
	payload := `rollingUsage:{status:"ok",resetInSec:100,usagePercent:7}` +
		`weeklyUsage:{status:"ok",resetInSec:200,usagePercent:8}`
	data, err := parseQuota(payload)
	if err != nil {
		t.Fatalf("parseQuota: %v", err)
	}
	if data.Rolling.UsagePercent != 7 || data.Weekly.UsagePercent != 8 {
		t.Errorf("unexpected parse: %+v", data)
	}
}

func TestParseQuotaMissingRolling(t *testing.T) {
	if _, err := parseQuota(`weeklyUsage:{status:"ok",resetInSec:1,usagePercent:1}`); err == nil {
		t.Fatal("parseQuota succeeded without rollingUsage, want error")
	}
}

func TestFetchQuota(t *testing.T) {
	var gotPath, gotArgs, gotCookie, gotServerID, gotInstance string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotArgs = r.URL.Query().Get("args")
		gotCookie = r.Header.Get("cookie")
		gotServerID = r.Header.Get("x-server-id")
		gotInstance = r.Header.Get("x-server-instance")
		w.Header().Set("content-type", "text/html")
		_, _ = w.Write([]byte(samplePayload))
	}))
	defer srv.Close()
	oldBase := baseURL
	baseURL = srv.URL
	defer func() { baseURL = oldBase }()

	data, err := FetchQuota(t.Context(), srv.Client(), "auth=abc123", "wrk_test")
	if err != nil {
		t.Fatalf("FetchQuota: %v", err)
	}
	if gotPath != "/_server" {
		t.Errorf("path = %q, want /_server", gotPath)
	}
	if !strings.Contains(gotArgs, `"f":31`) || !strings.Contains(gotArgs, `wrk_test`) {
		t.Errorf("args = %q, want function 31 with workspace id", gotArgs)
	}
	if gotCookie != "auth=abc123" {
		t.Errorf("cookie header = %q", gotCookie)
	}
	if gotServerID != serviceID {
		t.Errorf("x-server-id = %q, want %q", gotServerID, serviceID)
	}
	if gotInstance != serverFn {
		t.Errorf("x-server-instance = %q, want %q", gotInstance, serverFn)
	}
	if data.Rolling.UsagePercent != 30 {
		t.Errorf("rolling usagePercent = %d, want 30", data.Rolling.UsagePercent)
	}
	if data.FetchedAt.IsZero() {
		t.Error("FetchedAt is zero")
	}
}

func TestFetchQuotaHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	oldBase := baseURL
	baseURL = srv.URL
	defer func() { baseURL = oldBase }()

	_, err := FetchQuota(t.Context(), srv.Client(), "auth=abc", "wrk_test")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("FetchQuota err = %v, want 500 error", err)
	}
}

func TestFetchQuotaBadBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not seroval"))
	}))
	defer srv.Close()
	oldBase := baseURL
	baseURL = srv.URL
	defer func() { baseURL = oldBase }()

	if _, err := FetchQuota(t.Context(), srv.Client(), "auth=abc", "wrk_test"); err == nil {
		t.Fatal("FetchQuota succeeded on bad body, want error")
	}
}

func TestFetchQuotaAuthRedirect(t *testing.T) {
	// The server replies 200 with a serialized 302 to /auth/authorize when the
	// session cookie is invalid or expired (observed against live opencode.ai).
	const redirectPayload = `;0x000000c9;((self.$R=self.$R||{})["server-fn:3"]=[],($R=>$R[0]=new Response(null,$R[1]={headers:$R[2]=new Headers($R[3]=[$R[4]=["location","/auth/authorize"]]),status:302,statusText:"Found"}))($R["server-fn:3"]))`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/javascript")
		_, _ = w.Write([]byte(redirectPayload))
	}))
	defer srv.Close()
	oldBase := baseURL
	baseURL = srv.URL
	defer func() { baseURL = oldBase }()

	_, err := FetchQuota(t.Context(), srv.Client(), "auth=stale", "wrk_test")
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("err = %v, want ErrAuthRequired", err)
	}
}

func TestAuthRedirectDetection(t *testing.T) {
	if !authRedirect(`["location","/auth/authorize"]...status:302`) {
		t.Error("authRedirect missed the redirect payload")
	}
	if authRedirect(samplePayload) {
		t.Error("authRedirect matched a normal quota payload")
	}
}

func TestFetchQuotaMissingCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	if _, err := FetchQuota(t.Context(), srv.Client(), "", "wrk_test"); !errors.Is(err, ErrNoCookie) {
		t.Fatalf("empty cookie err = %v, want ErrNoCookie", err)
	}
	if _, err := FetchQuota(t.Context(), srv.Client(), "auth=abc", ""); !errors.Is(err, ErrNoWorkspace) {
		t.Fatalf("empty workspace err = %v, want ErrNoWorkspace", err)
	}
}
