package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// loginTimeoutSeconds is the maximum number of seconds the login command
// waits for the browser OAuth callback before timing out.
const loginTimeoutSeconds = 120

// httpRequestTimeout is the maximum time allowed for individual HTTP requests
// (provider discovery, token exchange) during the login flow. This is separate
// from loginTimeoutSeconds which governs the browser callback wait.
const httpRequestTimeout = 30 * time.Second

// Verbatim HTML responses for the OAuth callback server.
const (
	callbackSuccessHTML = `<html><body><h1>Login successful</h1><p>You may close this tab.</p></body></html>`
	callbackErrorHTML   = `<html><body><h1>Login failed</h1><p>OAuth state mismatch. Please try again.</p></body></html>`
)

// loginOpts holds configuration for the login OAuth flow.
// Fields with Fn suffix are dependency injection points for testing.
type loginOpts struct {
	provider      string
	expires       int
	endpointURL   string
	configPath    string
	openBrowserFn func(string) error
	saveConfigFn  func(string, *CLIConfig) error
	stderr        io.Writer
	stdout        io.Writer
}

// oauthProvider mirrors the SDK's OAuthProvider type for JSON decoding
// without creating an import cycle with the root apikit package.
type oauthProvider struct {
	Name         string `json:"name"`
	AuthorizeURL string `json:"authorize_url"`
}

// authCallbackRequest is the request body for the OAuth code exchange.
type authCallbackRequest struct {
	Provider    string `json:"provider"`
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
	Expires     *int   `json:"expires,omitempty"`
}

// authCallbackResponse mirrors the SDK's AuthCallbackResponse type.
type authCallbackResponse struct {
	User   *loginUser   `json:"user"`
	APIKey *loginAPIKey `json:"api_key"`
}

// loginUser mirrors the SDK's User type — only fields needed by login.
type loginUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	FullName   string `json:"full_name"`
	Status     string `json:"status"`
	Role       string `json:"role"`
	Provider   string `json:"provider"`
	ProviderID string `json:"provider_id"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	BlockedAt  string `json:"blocked_at,omitempty"`
}

// loginAPIKey mirrors the SDK's APIKeyFull type.
type loginAPIKey struct {
	Key       string `json:"key"`
	KeyID     string `json:"key_id"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// apiMountPoint is the default SDK API mount point.
const apiMountPoint = "/api/v1"

// NewLoginCmd returns the Cobra command for `akc login`.
func NewLoginCmd() *cobra.Command {
	var provider string
	var expires int

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate via browser-based OAuth",
		Long:  "Authenticate via browser-based OAuth and persist credentials to the config file.",
		Annotations: map[string]string{
			"auth":      "none",
			"composite": "true",
			// method and path are empty (null) for composite commands.
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve endpoint URL from flag, env, or config.
			endpointURL, _ := cmd.Flags().GetString("endpoint-url")
			if endpointURL == "" {
				endpointURL = ResolveEndpointURL(cmd)
			}

			home, err := os.UserHomeDir()
			if err != nil || home == "" {
				return fmt.Errorf("cannot determine home directory: $HOME is not set or unresolvable")
			}
			configDir := filepath.Join(home, "."+TokenPrefix)
			if err := InitConfig(configDir); err != nil {
				return err
			}

			opts := loginOpts{
				provider:      provider,
				expires:       expires,
				endpointURL:   endpointURL,
				configPath:    configDir,
				openBrowserFn: openBrowser,
				saveConfigFn:  SaveConfig,
				stderr:        cmd.ErrOrStderr(),
				stdout:        cmd.OutOrStdout(),
			}

			return runLogin(cmd.Context(), time.Duration(loginTimeoutSeconds)*time.Second, opts)
		},
	}

	cmd.Flags().StringVar(&provider, "provider", "github", "OAuth provider name")
	cmd.Flags().IntVar(&expires, "expires", 90, "Credential expiry in days (0, 30, 60, or 90)")

	return cmd
}

// runLogin implements the OAuth login flow with a configurable timeout.
// The Cobra RunE function calls this with time.Duration(loginTimeoutSeconds)*time.Second.
// Tests call this directly with a short timeout to exercise timeout behavior.
func runLogin(ctx context.Context, timeout time.Duration, opts loginOpts) error {
	// --- Precondition checks (before any network calls) ---

	if opts.endpointURL == "" {
		return fmt.Errorf("endpoint URL is required for login — use --endpoint-url or set ENDPOINT_URL")
	}

	if err := validateExpires(opts.expires); err != nil {
		return err
	}

	// --- Step 1: Discover providers ---

	baseURL := strings.TrimRight(opts.endpointURL, "/")
	providersURL := baseURL + apiMountPoint + "/auth/providers"

	providerCtx, providerCancel := context.WithTimeout(ctx, httpRequestTimeout)
	defer providerCancel()

	providerReq, err := http.NewRequestWithContext(providerCtx, http.MethodGet, providersURL, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch providers: %w", err)
	}

	providersResp, err := http.DefaultClient.Do(providerReq)
	if err != nil {
		return fmt.Errorf("failed to fetch providers: %w", err)
	}
	defer providersResp.Body.Close()

	if providersResp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch providers: server returned %s", providersResp.Status)
	}

	var providers []*oauthProvider
	if err := json.NewDecoder(providersResp.Body).Decode(&providers); err != nil {
		return fmt.Errorf("failed to decode providers response: %w", err)
	}

	// Find requested provider in the list.
	var authorizeURL string
	for _, p := range providers {
		if p.Name == opts.provider {
			authorizeURL = p.AuthorizeURL
			break
		}
	}
	if authorizeURL == "" {
		return fmt.Errorf("provider '%s' not found", opts.provider)
	}

	// --- Step 2: Generate state and start callback server ---

	state, err := generateState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	handler := newCallbackHandler(state, codeCh, errCh)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start callback server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	srv := &http.Server{Handler: handler}
	go func() {
		_ = srv.Serve(listener)
	}()

	// Always shut down the server using a fresh background context.
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	// --- Step 3: Build auth URL and open browser ---

	authURL, err := buildAuthURL(authorizeURL, redirectURI, state)
	if err != nil {
		return fmt.Errorf("failed to build authorization URL: %w", err)
	}

	fmt.Fprintln(opts.stderr, "Opening browser for authentication...")

	browserOpenFn := opts.openBrowserFn
	if browserOpenFn == nil {
		browserOpenFn = openBrowser
	}
	if browserErr := browserOpenFn(authURL); browserErr != nil {
		fmt.Fprintf(opts.stderr, "Open this URL in your browser: %s\n", authURL)
	}

	// --- Step 4: Wait for callback or timeout ---

	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, timeout)
	defer timeoutCancel()

	var code string
	select {
	case code = <-codeCh:
		// Received authorization code from callback.
	case cbErr := <-errCh:
		// State mismatch or other callback error.
		return fmt.Errorf("OAuth state mismatch — possible CSRF attack: %w", cbErr)
	case <-timeoutCtx.Done():
		return fmt.Errorf("login timed out waiting for browser callback")
	}

	// --- Step 5: Exchange code for credentials ---

	expires := opts.expires
	exchangeReq := &authCallbackRequest{
		Provider:    opts.provider,
		Code:        code,
		RedirectURI: redirectURI,
		Expires:     &expires,
	}

	reqBody, err := json.Marshal(exchangeReq)
	if err != nil {
		return fmt.Errorf("failed to marshal exchange request: %w", err)
	}

	exchangeURL := baseURL + apiMountPoint + "/auth/callback"

	exchangeCtx, exchangeCancel := context.WithTimeout(ctx, httpRequestTimeout)
	defer exchangeCancel()

	exchangeHTTPReq, err := http.NewRequestWithContext(exchangeCtx, http.MethodPost, exchangeURL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to exchange OAuth code: %w", err)
	}
	exchangeHTTPReq.Header.Set("Content-Type", "application/json")

	httpResp, err := http.DefaultClient.Do(exchangeHTTPReq)
	if err != nil {
		return fmt.Errorf("failed to exchange OAuth code: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(httpResp.Body).Decode(&errResp); err == nil && errResp.Error.Message != "" {
			return fmt.Errorf("login failed: %s", errResp.Error.Message)
		}
		return fmt.Errorf("login failed: server returned HTTP %d", httpResp.StatusCode)
	}

	var exchangeResp authCallbackResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&exchangeResp); err != nil {
		return fmt.Errorf("failed to decode exchange response: %w", err)
	}

	if exchangeResp.User == nil || exchangeResp.APIKey == nil {
		return fmt.Errorf("invalid exchange response: missing user or api_key")
	}

	// --- Step 6: Save config ---

	cfg := &CLIConfig{
		EndpointURL: opts.endpointURL,
		UserID:      exchangeResp.User.ID,
		APIKey:      exchangeResp.APIKey.Key,
	}

	saveFn := opts.saveConfigFn
	if saveFn == nil {
		saveFn = SaveConfig
	}
	if err := saveFn(opts.configPath, cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// --- Step 7: Output ---

	// Print user JSON to stdout (indented, no HTML escaping).
	enc := json.NewEncoder(opts.stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(exchangeResp.User); err != nil {
		return fmt.Errorf("failed to encode user JSON: %w", err)
	}

	fmt.Fprintf(opts.stderr, "Logged in as %s\n", exchangeResp.User.Username)

	return nil
}

// newCallbackHandler returns an HTTP handler for the OAuth callback server.
// The handler handles GET /callback by validating the state parameter and
// sending the code on codeCh (success) or an error on errCh (state mismatch).
// All other paths receive HTTP 404.
func newCallbackHandler(state string, codeCh chan string, errCh chan error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only handle the exact /callback path.
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}

		queryState := r.URL.Query().Get("state")
		if queryState != state {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, callbackErrorHTML)
			errCh <- fmt.Errorf("OAuth state mismatch")
			return
		}

		code := r.URL.Query().Get("code")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, callbackSuccessHTML)
		codeCh <- code
	})
}

// generateState generates a cryptographically random 64-character hex
// string for the OAuth state parameter using crypto/rand.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// buildAuthURL constructs the OAuth authorization URL by parsing the
// provider's authorize_url, preserving existing query parameters, and
// appending redirect_uri, state, and response_type=code.
// Does NOT add client_id — it is already in the authorize_url.
func buildAuthURL(authorizeURL, redirectURI, state string) (string, error) {
	u, err := url.Parse(authorizeURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("response_type", "code")
	u.RawQuery = q.Encode()
	return u.String(), nil
}
