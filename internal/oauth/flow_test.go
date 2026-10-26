package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/credentials"
)

const dashboardURL = "http://localhost:3001"

func TestCodeChallengeMatchesRFC7636AppendixB(t *testing.T) {
	if got := codeChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("codeChallenge() = %q", got)
	}
}

func TestNewVerifierIsUnreservedAndLongEnough(t *testing.T) {
	verifier, err := randomString()
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Fatalf("verifier length = %d", len(verifier))
	}
	if strings.Trim(verifier, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~") != "" {
		t.Fatalf("verifier %q has reserved characters", verifier)
	}
}

type fakeServer struct {
	*httptest.Server
	t             *testing.T
	mu            sync.Mutex
	challenge     string
	redirectURI   string
	tokenRequests []url.Values
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fake := &fakeServer{t: t}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.Close)
	return fake
}

func (f *fakeServer) issuer() string { return f.URL }

func (f *fakeServer) serve(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/.well-known/oauth-authorization-server":
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer":                 f.issuer(),
			"authorization_endpoint": f.issuer() + "/oauth2/authorize",
			"token_endpoint":         f.issuer() + "/oauth2/token",
			"revocation_endpoint":    f.issuer() + "/oauth2/revoke",
			"scopes_supported":       []string{"mcp", "api"},
		})
	case "/oauth2/token":
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			f.t.Errorf("token request %s %q", request.Method, request.Header.Get("Content-Type"))
		}
		if err := request.ParseForm(); err != nil {
			f.t.Errorf("parse form: %v", err)
		}
		f.mu.Lock()
		f.tokenRequests = append(f.tokenRequests, request.PostForm)
		challenge, redirectURI := f.challenge, f.redirectURI
		f.mu.Unlock()
		form := request.PostForm
		if form.Get("client_id") != f.issuer()+"/.well-known/oauth-client/frbit-cli" {
			writeError(writer, http.StatusBadRequest, "invalid_client")
			return
		}
		switch form.Get("grant_type") {
		case "authorization_code":
			if form.Get("code") != "the-code" || form.Get("redirect_uri") != redirectURI || codeChallenge(form.Get("code_verifier")) != challenge {
				writeError(writer, http.StatusBadRequest, "invalid_grant")
				return
			}
			writeTokens(writer, "access-1", "refresh-1")
		case "refresh_token":
			if form.Get("refresh_token") != "refresh-1" {
				writeError(writer, http.StatusBadRequest, "invalid_grant")
				return
			}
			writeTokens(writer, "access-2", "refresh-2")
		default:
			writeError(writer, http.StatusBadRequest, "unsupported_grant_type")
		}
	default:
		http.NotFound(writer, request)
	}
}

func writeTokens(writer http.ResponseWriter, access string, refresh string) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "refresh_token": refresh})
}

func writeError(writer http.ResponseWriter, status int, code string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": code})
}

// browser plays the person: it checks the authorize URL and calls the loopback callback.
type browser struct {
	t         *testing.T
	fake      *fakeServer
	callbacks []func(redirectURI string, state string) url.Values
	responses chan *http.Response
}

func (b *browser) open(rawURL string) error {
	authorize, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	query := authorize.Query()
	issuer := b.fake.issuer()
	expected := map[string]string{
		"client_id":             issuer + "/.well-known/oauth-client/frbit-cli",
		"response_type":         "code",
		"scope":                 "api",
		"resource":              issuer + "/v1",
		"code_challenge_method": "S256",
	}
	for key, value := range expected {
		if query.Get(key) != value {
			b.t.Errorf("authorize %s = %q, want %q", key, query.Get(key), value)
		}
	}
	if authorize.Path != "/oauth2/authorize" {
		b.t.Errorf("authorize path = %q", authorize.Path)
	}
	redirectURI := query.Get("redirect_uri")
	if !strings.HasPrefix(redirectURI, "http://127.0.0.1:") || !strings.HasSuffix(redirectURI, "/callback") {
		b.t.Errorf("redirect_uri = %q", redirectURI)
	}
	b.fake.mu.Lock()
	b.fake.challenge = query.Get("code_challenge")
	b.fake.redirectURI = redirectURI
	b.fake.mu.Unlock()

	go func() {
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		for _, callback := range b.callbacks {
			response, err := client.Get(redirectURI + "?" + callback(redirectURI, query.Get("state")).Encode())
			if err != nil {
				b.t.Errorf("callback: %v", err)
				return
			}
			_ = response.Body.Close()
			b.responses <- response
		}
	}()
	return nil
}

func validCallback(_ string, state string) url.Values {
	return url.Values{"code": {"the-code"}, "state": {state}}
}

func TestLoginCompletesBrowserFlow(t *testing.T) {
	fake := newFakeServer(t)
	person := &browser{t: t, fake: fake, callbacks: []func(string, string) url.Values{validCallback}, responses: make(chan *http.Response, 1)}
	shown := ""
	host := strings.Replace(fake.URL, "127.0.0.1", "localhost", 1) + "/"

	credential, err := Login(context.Background(), Options{
		Host:         host,
		DashboardURL: dashboardURL,
		OpenBrowser:  person.open,
		Out:          &strings.Builder{},
		Timeout:      5 * time.Second,
		OnURL:        func(rawURL string) error { shown = rawURL; return nil },
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if credential.Kind != credentials.KindOAuth || credential.AccessToken != "access-1" || credential.RefreshToken != "refresh-1" {
		t.Fatalf("credential = %#v", credential)
	}
	if until := time.Until(credential.ExpiresAt); until < 59*time.Minute || until > time.Hour {
		t.Fatalf("expires in %s", until)
	}
	if !strings.HasPrefix(shown, fake.issuer()+"/oauth2/authorize?") {
		t.Fatalf("shown URL = %q", shown)
	}
	response := <-person.responses
	if response.StatusCode != http.StatusFound || response.Header.Get("Location") != dashboardURL+"/oauth2/connected?client=frbit-cli" {
		t.Fatalf("callback response = %d %q", response.StatusCode, response.Header.Get("Location"))
	}
}

func TestLoginIgnoresWrongState(t *testing.T) {
	fake := newFakeServer(t)
	wrongState := func(string, string) url.Values { return url.Values{"code": {"stolen"}, "state": {"forged"}} }
	person := &browser{t: t, fake: fake, callbacks: []func(string, string) url.Values{wrongState, validCallback}, responses: make(chan *http.Response, 2)}

	credential, err := Login(context.Background(), Options{Host: fake.URL, DashboardURL: dashboardURL, OpenBrowser: person.open, Out: &strings.Builder{}, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if credential.AccessToken != "access-1" {
		t.Fatalf("credential = %#v", credential)
	}
	if first := <-person.responses; first.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong state answered %d", first.StatusCode)
	}
	if second := <-person.responses; second.StatusCode != http.StatusFound {
		t.Fatalf("matching state answered %d", second.StatusCode)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.tokenRequests) != 1 || fake.tokenRequests[0].Get("code") != "the-code" {
		t.Fatalf("token requests = %#v", fake.tokenRequests)
	}
}

func TestLoginReportsDeniedConsent(t *testing.T) {
	fake := newFakeServer(t)
	denied := func(_ string, state string) url.Values {
		return url.Values{"error": {"access_denied"}, "state": {state}}
	}
	person := &browser{t: t, fake: fake, callbacks: []func(string, string) url.Values{denied}, responses: make(chan *http.Response, 1)}

	_, err := Login(context.Background(), Options{Host: fake.URL, DashboardURL: dashboardURL, OpenBrowser: person.open, Out: &strings.Builder{}, Timeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("Login() error = %v", err)
	}
}

func TestLoginPrintsURLWhenBrowserIsUnavailable(t *testing.T) {
	fake := newFakeServer(t)
	output := &strings.Builder{}

	_, err := Login(context.Background(), Options{
		Host: fake.URL, DashboardURL: dashboardURL, Out: output, Timeout: 50 * time.Millisecond,
		OpenBrowser: func(string) error { return errors.New("no browser") },
	})
	if err == nil {
		t.Fatal("Login() succeeded without a callback")
	}
	if !strings.Contains(output.String(), fake.issuer()+"/oauth2/authorize?") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestLoginTimeoutNamesTokenStdin(t *testing.T) {
	fake := newFakeServer(t)

	_, err := Login(context.Background(), Options{Host: fake.URL, DashboardURL: dashboardURL, Out: &strings.Builder{}, Timeout: 50 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "--token-stdin") {
		t.Fatalf("Login() error = %v", err)
	}
}

func TestLoginRejectsHostWithoutAPIScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer":                 "http://" + request.Host,
			"authorization_endpoint": "http://" + request.Host + "/oauth2/authorize",
			"token_endpoint":         "http://" + request.Host + "/oauth2/token",
			"scopes_supported":       []string{"mcp"},
		})
	}))
	defer server.Close()

	_, err := Login(context.Background(), Options{Host: server.URL, DashboardURL: dashboardURL, Out: &strings.Builder{}, Timeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "--token-stdin") {
		t.Fatalf("Login() error = %v", err)
	}
}

func TestRefreshRotatesTokens(t *testing.T) {
	fake := newFakeServer(t)

	credential, err := Refresh(context.Background(), fake.URL, nil, "refresh-1")
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if credential.Kind != credentials.KindOAuth || credential.AccessToken != "access-2" || credential.RefreshToken != "refresh-2" {
		t.Fatalf("credential = %#v", credential)
	}

	_, err = Refresh(context.Background(), fake.URL, nil, "refresh-1-consumed")
	var tokenErr *TokenError
	if !errors.As(err, &tokenErr) || tokenErr.Code != "invalid_grant" {
		t.Fatalf("Refresh() error = %v", err)
	}
}
