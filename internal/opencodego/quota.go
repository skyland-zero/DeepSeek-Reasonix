package opencodego

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	serviceID = "c7389bd0e731f80f49593e5ee53835475f4e28594dd6bd83eb229bab753498cd"
	serverFn  = "server-fn:3"
)

// baseURL is a var so tests can point it at a local httptest.Server.
var baseURL = "https://opencode.ai"

// FetchQuota calls the opencode.ai server-function RPC endpoint and returns
// the parsed quota readout. The session cookie and workspace id are required;
// resolve them with ResolveCredentials.
func FetchQuota(ctx context.Context, client *http.Client, cookie, workspaceID string) (*QuotaData, error) {
	if cookie == "" {
		return nil, ErrNoCookie
	}
	if workspaceID == "" {
		return nil, ErrNoWorkspace
	}
	reqURL := fmt.Sprintf("%s/_server?id=%s&args=%s", baseURL, serviceID, url.QueryEscape(buildRPCArgs(workspaceID)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("opencode go request: %w", err)
	}
	req.Header.Set("accept", "*/*")
	req.Header.Set("cookie", cookie)
	req.Header.Set("x-server-id", serviceID)
	req.Header.Set("x-server-instance", serverFn)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode go query: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("opencode go API returned %d: %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("opencode go response: %w", err)
	}
	if authRedirect(string(body)) {
		return nil, ErrAuthRequired
	}
	data, err := parseQuota(string(body))
	if err != nil {
		return nil, err
	}
	data.FetchedAt = time.Now()
	return data, nil
}

// buildRPCArgs encodes the seroval-style RPC invocation for the quota server
// function (function 31 takes the workspace id).
func buildRPCArgs(workspaceID string) string {
	args, _ := json.Marshal(map[string]any{
		"t": map[string]any{"t": 9, "i": 0, "l": 1, "a": []any{map[string]any{"t": 1, "s": workspaceID}}, "o": 0},
		"f": 31,
		"m": []any{},
	})
	return string(args)
}
