package opencodego

import (
	"errors"
	"os"

	"reasonix/internal/config"
)

// Credential keys for the OpenCode Go session cookie and workspace id. They
// are stored through the shared skycode credential store (user-level .env /
// keyring), so one setup serves every project.
const (
	CookieEnv    = "OPENCODE_GO_AUTH_COOKIE"
	WorkspaceEnv = "OPENCODE_GO_WORKSPACE_ID"
)

var (
	ErrNoCookie    = errors.New("OPENCODE_GO_AUTH_COOKIE is not set")
	ErrNoWorkspace = errors.New("OPENCODE_GO_WORKSPACE_ID is not set")
	// ErrAuthRequired reports that opencode.ai rejected the session cookie;
	// the RPC payload carried a serialized 302 redirect to /auth/authorize.
	ErrAuthRequired = errors.New("OpenCode Go session is invalid or expired — re-run /quota setup with a fresh cookie from https://opencode.ai/auth")
)

// Credentials is the resolved session material for one query.
type Credentials struct {
	Cookie      string
	WorkspaceID string
}

// ResolveCredentials returns the cookie/workspace pair, preferring live
// environment variables over the stored credential store.
func ResolveCredentials() Credentials {
	return Credentials{
		Cookie:      resolveCredential(CookieEnv),
		WorkspaceID: resolveCredential(WorkspaceEnv),
	}
}

// Configured reports whether a full credential pair is available.
func Configured() bool {
	c := ResolveCredentials()
	return c.Cookie != "" && c.WorkspaceID != ""
}

func resolveCredential(key string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if res := config.ResolveCredentialForRootGlobalFirst(".", key); res.Set {
		return res.Value
	}
	return ""
}

// StoreCredentials persists the pair into the shared credential store.
func StoreCredentials(cookie, workspaceID string) error {
	if _, err := config.SetCredential(CookieEnv, cookie); err != nil {
		return err
	}
	if _, err := config.SetCredential(WorkspaceEnv, workspaceID); err != nil {
		return err
	}
	return nil
}

// ClearCredentials removes the pair from the shared credential store.
func ClearCredentials() error {
	if err := config.RemoveCredential(CookieEnv); err != nil {
		return err
	}
	return config.RemoveCredential(WorkspaceEnv)
}
