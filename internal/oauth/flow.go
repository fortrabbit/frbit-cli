// Package oauth signs the CLI in through the fortrabbit OAuth server: loopback redirect, PKCE S256, public client.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fortrabbit/frbit-cli/internal/credentials"
)

const (
	DefaultTimeout   = 15 * time.Minute
	scope            = "api"
	clientIDPath     = "/.well-known/oauth-client/frbit-cli"
	metadataPath     = "/.well-known/oauth-authorization-server"
	connectedPath    = "/oauth2/connected?client=frbit-cli"
	maxResponseBytes = 1 << 20
)

type Options struct {
	Host         string
	DashboardURL string
	OpenBrowser  func(string) error
	Out          io.Writer
	Timeout      time.Duration
	HTTPClient   *http.Client
	// OnURL replaces the default sign-in prompt on Out, for machine-readable output.
	OnURL func(string) error
	// Signup sends prompt=create, so a person without an account lands on the sign-up page.
	Signup bool
	// NameHint suggests the name for a new account, sent as name_hint.
	NameHint string
}

// ValidateNameHint checks the name suggestion the way FRAPI does: 1 to 100 characters, no control characters.
func ValidateNameHint(name string) error {
	if length := utf8.RuneCountInString(name); length < 1 || length > 100 {
		return errors.New("--name must be 1 to 100 characters")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("--name must not contain control characters")
		}
	}
	return nil
}

type TokenError struct {
	Status      int
	Code        string
	Description string
}

func (e *TokenError) Error() string {
	message := fmt.Sprintf("token request failed with HTTP %d", e.Status)
	if e.Code != "" {
		message += ": " + e.Code
	}
	if e.Description != "" {
		message += ": " + e.Description
	}
	return message
}

type serverMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

func (m serverMetadata) clientID() string {
	return strings.TrimRight(m.Issuer, "/") + clientIDPath
}

// SameOrigin reports whether two absolute URLs share scheme, host and effective port.
func SameOrigin(first string, second string) bool {
	a, errA := url.Parse(first)
	b, errB := url.Parse(second)
	if errA != nil || errB != nil || a.Host == "" || b.Host == "" {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && effectivePort(a) == effectivePort(b)
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "http") {
		return "80"
	}
	return "443"
}

func (m serverMetadata) resource() string {
	return strings.TrimRight(m.Issuer, "/") + "/v1"
}

type callbackResult struct {
	credential credentials.Credential
	err        error
}

// Login waits for the person to sign in through the browser and returns the issued OAuth credential.
func Login(ctx context.Context, options Options) (credentials.Credential, error) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out := options.Out
	if out == nil {
		out = io.Discard
	}

	if options.NameHint != "" {
		if err := ValidateNameHint(options.NameHint); err != nil {
			return credentials.Credential{}, err
		}
	}
	client := tokenClient(options.HTTPClient)
	metadata, err := discover(ctx, client, options.Host)
	if err != nil {
		return credentials.Credential{}, err
	}
	connectedURL, err := connectedURL(options.DashboardURL)
	if err != nil {
		return credentials.Credential{}, err
	}
	verifier, err := randomString()
	if err != nil {
		return credentials.Credential{}, err
	}
	state, err := randomString()
	if err != nil {
		return credentials.Credential{}, err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("listen for the browser callback: %w", err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
	authorizeURL, err := metadata.authorizeURL(redirectURI, codeChallenge(verifier), state, options)
	if err != nil {
		_ = listener.Close()
		return credentials.Credential{}, err
	}

	results := make(chan callbackResult, 1)
	var accepted sync.Mutex
	done := false
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writePage(writer, http.StatusMethodNotAllowed, "Method not allowed.")
			return
		}
		query := request.URL.Query()
		if subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(state)) != 1 {
			writePage(writer, http.StatusBadRequest, "This sign-in does not belong to the waiting frbit CLI. Start again from your terminal.")
			return
		}
		accepted.Lock()
		if done {
			accepted.Unlock()
			writePage(writer, http.StatusConflict, "frbit CLI has already received this sign-in. Return to your terminal.")
			return
		}
		done = true
		accepted.Unlock()

		credential, err := completeCallback(ctx, client, metadata, query, redirectURI, verifier)
		if err != nil {
			writePage(writer, http.StatusBadRequest, "frbit CLI could not sign in: "+err.Error()+"\nReturn to your terminal.")
			results <- callbackResult{err: err}
			return
		}
		http.Redirect(writer, request, connectedURL, http.StatusFound)
		results <- callbackResult{credential: credential}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if options.OnURL != nil {
		if err := options.OnURL(authorizeURL); err != nil {
			return credentials.Credential{}, err
		}
	} else if _, err := fmt.Fprintf(out, "Open this URL to sign in to fortrabbit:\n%s\n\n", authorizeURL); err != nil {
		return credentials.Credential{}, err
	}
	if options.OpenBrowser != nil {
		if err := options.OpenBrowser(authorizeURL); err != nil {
			_, _ = fmt.Fprintln(out, "Could not open a browser. Open the URL above to continue.")
		}
	}
	_, _ = fmt.Fprintf(out, "Waiting up to %s for the browser sign-in…\n", humanDuration(timeout))

	select {
	case result := <-results:
		return result.credential, result.err
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return credentials.Credential{}, fmt.Errorf("no browser sign-in within %s; in CI or without a browser, pipe an API token to `frbit auth login --token-stdin` or set FRBIT_TOKEN", humanDuration(timeout))
		}
		return credentials.Credential{}, ctx.Err()
	}
}

// Refresh trades a refresh token for a new access and refresh token pair.
func Refresh(ctx context.Context, host string, httpClient *http.Client, refreshToken string) (credentials.Credential, error) {
	client := tokenClient(httpClient)
	metadata, err := discover(ctx, client, host)
	if err != nil {
		return credentials.Credential{}, err
	}
	credential, err := requestTokens(ctx, client, metadata, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {metadata.clientID()},
	})
	if err != nil {
		return credentials.Credential{}, err
	}
	if credential.RefreshToken == "" {
		credential.RefreshToken = refreshToken
	}
	return credential, nil
}

func completeCallback(ctx context.Context, client *http.Client, metadata serverMetadata, query url.Values, redirectURI string, verifier string) (credentials.Credential, error) {
	if code := query.Get("error"); code != "" {
		if description := query.Get("error_description"); description != "" {
			code += ": " + description
		}
		return credentials.Credential{}, fmt.Errorf("sign-in was not completed: %s", code)
	}
	if issuer := query.Get("iss"); issuer != "" && issuer != metadata.Issuer {
		return credentials.Credential{}, fmt.Errorf("sign-in came from issuer %q, expected %q", issuer, metadata.Issuer)
	}
	code := query.Get("code")
	if code == "" {
		return credentials.Credential{}, errors.New("sign-in returned no authorization code")
	}
	return requestTokens(ctx, client, metadata, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {metadata.clientID()},
		"code_verifier": {verifier},
	})
}

func discover(ctx context.Context, client *http.Client, host string) (serverMetadata, error) {
	origin, err := checkEndpoint(strings.TrimRight(strings.TrimSpace(host), "/"))
	if err != nil {
		return serverMetadata{}, fmt.Errorf("API host: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin.String()+metadataPath, nil)
	if err != nil {
		return serverMetadata{}, fmt.Errorf("create discovery request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return serverMetadata{}, fmt.Errorf("discover the sign-in server: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return serverMetadata{}, fmt.Errorf("read sign-in server metadata: %w", err)
	}
	unsupported := fmt.Errorf("%s does not offer browser sign-in for the CLI; pipe an API token to `frbit auth login --token-stdin` instead", origin)
	if response.StatusCode == http.StatusNotFound {
		return serverMetadata{}, unsupported
	}
	if response.StatusCode != http.StatusOK {
		return serverMetadata{}, fmt.Errorf("discover the sign-in server: HTTP %d", response.StatusCode)
	}

	var metadata serverMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return serverMetadata{}, fmt.Errorf("decode sign-in server metadata: %w", err)
	}
	if len(metadata.ScopesSupported) > 0 && !slices.Contains(metadata.ScopesSupported, scope) {
		return serverMetadata{}, unsupported
	}
	if _, err := checkEndpoint(metadata.Issuer); err != nil {
		return serverMetadata{}, fmt.Errorf("sign-in server metadata issuer: %w", err)
	}
	if !SameOrigin(metadata.Issuer, origin.String()) {
		return serverMetadata{}, fmt.Errorf("the sign-in metadata of %s names another issuer, %s; sign in against that host or check the API host", origin, metadata.Issuer)
	}
	for name, value := range map[string]string{"authorization_endpoint": metadata.AuthorizationEndpoint, "token_endpoint": metadata.TokenEndpoint} {
		if _, err := checkEndpoint(value); err != nil {
			return serverMetadata{}, fmt.Errorf("sign-in server metadata %s: %w", name, err)
		}
		if !SameOrigin(value, metadata.Issuer) {
			return serverMetadata{}, fmt.Errorf("sign-in server metadata %s %q is not on the issuer's origin %q", name, value, metadata.Issuer)
		}
	}
	return metadata, nil
}

func (m serverMetadata) authorizeURL(redirectURI string, challenge string, state string, options Options) (string, error) {
	endpoint, err := url.Parse(m.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse authorization endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("client_id", m.clientID())
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", scope)
	query.Set("resource", m.resource())
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	if options.Signup {
		query.Set("prompt", "create")
	}
	if options.NameHint != "" {
		query.Set("name_hint", options.NameHint)
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func requestTokens(ctx context.Context, client *http.Client, metadata serverMetadata, form url.Values) (credentials.Credential, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, metadata.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("create token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("perform token request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("read token response: %w", err)
	}

	var payload struct {
		AccessToken      string `json:"access_token"`
		TokenType        string `json:"token_type"`
		ExpiresIn        int64  `json:"expires_in"`
		RefreshToken     string `json:"refresh_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &payload)
	if response.StatusCode != http.StatusOK {
		return credentials.Credential{}, &TokenError{Status: response.StatusCode, Code: payload.Error, Description: payload.ErrorDescription}
	}
	if payload.AccessToken == "" {
		return credentials.Credential{}, errors.New("token response has no access token")
	}
	if payload.TokenType != "" && !strings.EqualFold(payload.TokenType, "Bearer") {
		return credentials.Credential{}, fmt.Errorf("token response has unsupported token type %q", payload.TokenType)
	}
	credential := credentials.Credential{Kind: credentials.KindOAuth, AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken, Issuer: metadata.Issuer}
	if payload.ExpiresIn > 0 {
		credential.ExpiresAt = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second).UTC()
	}
	return credential, nil
}

func tokenClient(httpClient *http.Client) *http.Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	clone := *httpClient
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &clone
}

// checkEndpoint allows HTTPS anywhere and plain HTTP only on loopback, as the API client does.
func checkEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("invalid URL %q", raw)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return parsed, nil
	case "http":
		if isLoopback(parsed.Hostname()) {
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("%q must use HTTPS; HTTP is allowed only for loopback addresses", raw)
}

func isLoopback(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func connectedURL(dashboardURL string) (string, error) {
	parsed, err := url.Parse(dashboardURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("invalid dashboard URL %q", dashboardURL)
	}
	return strings.TrimRight(dashboardURL, "/") + connectedPath, nil
}

func humanDuration(duration time.Duration) string {
	if duration < time.Minute || duration%time.Minute != 0 {
		return duration.String()
	}
	if minutes := int(duration / time.Minute); minutes != 1 {
		return fmt.Sprintf("%d minutes", minutes)
	}
	return "1 minute"
}

func randomString() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate random value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writePage(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = io.WriteString(writer, message+"\n")
}
