package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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
	revokeStatus  int
	revokeForms   []url.Values
	// exchanging, when set, receives a signal and blocks the token endpoint until it is closed.
	exchanging chan chan struct{}
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
	case "/oauth2/revoke":
		_ = request.ParseForm()
		f.mu.Lock()
		f.revokeForms = append(f.revokeForms, request.PostForm)
		status := f.revokeStatus
		f.mu.Unlock()
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			f.t.Errorf("revoke request %s %q", request.Method, request.Header.Get("Content-Type"))
		}
		if status != 0 {
			writer.WriteHeader(status)
		}
	case "/oauth2/token":
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			f.t.Errorf("token request %s %q", request.Method, request.Header.Get("Content-Type"))
		}
		if err := request.ParseForm(); err != nil {
			f.t.Errorf("parse form: %v", err)
		}
		if f.exchanging != nil {
			release := make(chan struct{})
			f.exchanging <- release
			<-release
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
			values := callback(redirectURI, query.Get("state"))
			if _, set := values["iss"]; !set {
				values.Set("iss", b.fake.issuer())
			}
			response, err := client.Get(redirectURI + "?" + values.Encode())
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
	shown, shownCode := "", ""
	host := fake.URL + "/"

	credential, err := Login(context.Background(), Options{
		Host:         host,
		DashboardURL: dashboardURL,
		OpenBrowser:  person.open,
		Out:          &strings.Builder{},
		Timeout:      5 * time.Second,
		OnURL:        func(rawURL string, code string) error { shown, shownCode = rawURL, code; return nil },
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if credential.Kind != credentials.KindOAuth || credential.AccessToken != "access-1" || credential.RefreshToken != "refresh-1" || credential.Issuer != fake.issuer() {
		t.Fatalf("credential = %#v", credential)
	}
	if until := time.Until(credential.ExpiresAt); until < 59*time.Minute || until > time.Hour {
		t.Fatalf("expires in %s", until)
	}
	if !strings.HasPrefix(shown, fake.issuer()+"/oauth2/authorize?") {
		t.Fatalf("shown URL = %q", shown)
	}
	authorize, _ := url.Parse(shown)
	if got := authorize.Query().Get("confirmation_challenge"); got != confirmationChallenge(shownCode, authorize.Query().Get("code_challenge")) {
		t.Fatalf("confirmation_challenge = %q for code %q", got, shownCode)
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

func TestLoginRejectsMissingOrWrongIssuer(t *testing.T) {
	for name, issuer := range map[string]string{"missing": "", "wrong": "https://evil.example"} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeServer(t)
			callback := func(_ string, state string) url.Values {
				return url.Values{"code": {"the-code"}, "state": {state}, "iss": {issuer}}
			}
			person := &browser{t: t, fake: fake, callbacks: []func(string, string) url.Values{callback}, responses: make(chan *http.Response, 1)}

			_, err := Login(context.Background(), Options{Host: fake.URL, DashboardURL: dashboardURL, OpenBrowser: person.open, Out: &strings.Builder{}, Timeout: 5 * time.Second})
			if err == nil || !strings.Contains(err.Error(), "sign-in came from issuer") {
				t.Fatalf("Login() error = %v", err)
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			if len(fake.tokenRequests) != 0 {
				t.Fatalf("token requests = %#v", fake.tokenRequests)
			}
		})
	}
}

func TestLoginReportsDeniedConsent(t *testing.T) {
	fake := newFakeServer(t)
	denied := func(_ string, state string) url.Values {
		return url.Values{"error": {"access_denied"}, "state": {state}}
	}
	person := &browser{t: t, fake: fake, callbacks: []func(string, string) url.Values{denied}, responses: make(chan *http.Response, 1)}

	_, err := Login(context.Background(), Options{Host: fake.URL, DashboardURL: dashboardURL, OpenBrowser: person.open, Out: &strings.Builder{}, Timeout: 5 * time.Second})
	if err == nil || !strings.Contains(err.Error(), "access_denied") || ErrorCode(err) != CodeAccessDenied {
		t.Fatalf("Login() error = %v, code %q", err, ErrorCode(err))
	}
}

func TestDeniedConsentAnswersPlainPageWithoutRedirect(t *testing.T) {
	fake := newFakeServer(t)
	type answer struct {
		page, contentType, location string
		status                      int
	}
	answers := make(chan answer, 1)
	opened := func(rawURL string) error {
		authorize, _ := url.Parse(rawURL)
		query := authorize.Query()
		go func() {
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			target := query.Get("redirect_uri") + "?" + url.Values{"error": {"access_denied"}, "state": {query.Get("state")}, "redirect_to": {"https://evil.example/"}, "iss": {fake.issuer()}}.Encode()
			response, err := client.Get(target)
			if err != nil {
				t.Errorf("callback: %v", err)
				return
			}
			defer response.Body.Close()
			body, _ := io.ReadAll(response.Body)
			answers <- answer{string(body), response.Header.Get("Content-Type"), response.Header.Get("Location"), response.StatusCode}
		}()
		return nil
	}

	_, err := Login(context.Background(), Options{Host: fake.URL, DashboardURL: dashboardURL, OpenBrowser: opened, Out: &strings.Builder{}, Timeout: 5 * time.Second})
	if ErrorCode(err) != CodeAccessDenied {
		t.Fatalf("Login() error = %v", err)
	}
	got := <-answers
	if got.status != http.StatusOK || !strings.HasPrefix(got.contentType, "text/plain") || got.location != "" {
		t.Fatalf("status %d, type %q, location %q", got.status, got.contentType, got.location)
	}
	if strings.TrimSpace(got.page) != "Not connected. Your agent knows; you can close this tab." {
		t.Fatalf("page = %q", got.page)
	}
}

func TestRevokePostsRefreshToken(t *testing.T) {
	fake := newFakeServer(t)

	if err := Revoke(context.Background(), fake.URL, nil, "refresh-1"); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.revokeForms) != 1 {
		t.Fatalf("revoke requests = %d", len(fake.revokeForms))
	}
	form := fake.revokeForms[0]
	if form.Get("token") != "refresh-1" || form.Get("token_type_hint") != "refresh_token" || form.Get("client_id") != fake.issuer()+"/.well-known/oauth-client/frbit-cli" {
		t.Fatalf("revoke form = %v", form)
	}
}

func TestRevokeReportsServerError(t *testing.T) {
	fake := newFakeServer(t)
	fake.revokeStatus = http.StatusInternalServerError

	if err := Revoke(context.Background(), fake.URL, nil, "refresh-1"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("Revoke() error = %v", err)
	}
}

func TestRevokeFailsWithoutEndpoint(t *testing.T) {
	server := metadataServer(t, func(origin string) map[string]any {
		return map[string]any{"issuer": origin, "authorization_endpoint": origin + "/a", "token_endpoint": origin + "/t"}
	})

	if err := Revoke(context.Background(), server.URL, nil, "refresh-1"); err == nil || !strings.Contains(err.Error(), "revocation") {
		t.Fatalf("Revoke() error = %v", err)
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
	if err == nil || !strings.Contains(err.Error(), "--token-stdin") || ErrorCode(err) != CodeTimeout {
		t.Fatalf("Login() error = %v, code %q", err, ErrorCode(err))
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

func TestLoginAnswersReplayAndOtherPaths(t *testing.T) {
	fake := newFakeServer(t)
	fake.exchanging = make(chan chan struct{})
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	statuses := make(chan int, 4)
	open := func(rawURL string) error {
		authorize, _ := url.Parse(rawURL)
		query := authorize.Query()
		fake.mu.Lock()
		fake.challenge, fake.redirectURI = query.Get("code_challenge"), query.Get("redirect_uri")
		fake.mu.Unlock()
		callback := query.Get("redirect_uri") + "?" + url.Values{"code": {"the-code"}, "state": {query.Get("state")}, "iss": {fake.issuer()}}.Encode()
		get := func(target string) {
			response, err := client.Get(target)
			if err != nil {
				t.Errorf("GET %s: %v", target, err)
				statuses <- 0
				return
			}
			_ = response.Body.Close()
			statuses <- response.StatusCode
		}
		go func() {
			go get(callback)
			release := <-fake.exchanging
			get(callback)
			get(strings.TrimSuffix(query.Get("redirect_uri"), "/callback") + "/favicon.ico")
			if response, err := client.Post(callback, "text/plain", nil); err == nil {
				_ = response.Body.Close()
				statuses <- response.StatusCode
			} else {
				t.Errorf("POST callback: %v", err)
				statuses <- 0
			}
			close(release)
		}()
		return nil
	}

	if _, err := Login(context.Background(), Options{Host: fake.URL, DashboardURL: dashboardURL, OpenBrowser: open, Out: &strings.Builder{}, Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	got := []int{<-statuses, <-statuses, <-statuses, <-statuses}
	want := map[int]bool{http.StatusConflict: true, http.StatusNotFound: true, http.StatusMethodNotAllowed: true, http.StatusFound: true}
	for _, status := range got {
		if !want[status] {
			t.Fatalf("statuses = %v", got)
		}
		delete(want, status)
	}
}

func metadataServer(t *testing.T, metadata func(origin string) map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(metadata("http://" + request.Host))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDiscoverRejectsEndpointsOffTheIssuerOrigin(t *testing.T) {
	server := metadataServer(t, func(origin string) map[string]any {
		return map[string]any{"issuer": origin, "authorization_endpoint": origin + "/oauth2/authorize", "token_endpoint": "https://evil.example/oauth2/token"}
	})

	_, err := discover(context.Background(), server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "token_endpoint") {
		t.Fatalf("discover() error = %v", err)
	}
}

func TestDiscoverAcceptsMissingScopesSupported(t *testing.T) {
	server := metadataServer(t, func(origin string) map[string]any {
		return map[string]any{"issuer": origin, "authorization_endpoint": origin + "/oauth2/authorize", "token_endpoint": origin + "/oauth2/token"}
	})

	if _, err := discover(context.Background(), server.Client(), server.URL); err != nil {
		t.Fatalf("discover() error = %v", err)
	}
}

func TestSameOrigin(t *testing.T) {
	for _, test := range []struct {
		first, second string
		want          bool
	}{
		{"https://api.fortrabbit.com", "https://API.fortrabbit.com:443/v1", true},
		{"http://127.0.0.1:8085", "http://127.0.0.1:8085/", true},
		{"https://api.fortrabbit.com", "https://evil.example", false},
		{"http://127.0.0.1:8085", "http://127.0.0.1:8086", false},
		{"https://api.fortrabbit.com", "", false},
	} {
		if got := SameOrigin(test.first, test.second); got != test.want {
			t.Errorf("SameOrigin(%q, %q) = %t", test.first, test.second, got)
		}
	}
}

func TestDiscoverRejectsMetadataNamingAnotherIssuer(t *testing.T) {
	// A local FRAPI configured with the production issuer must not send the browser or the code exchange there.
	server := metadataServer(t, func(string) map[string]any {
		return map[string]any{
			"issuer":                 "https://api.fortrabbit.com",
			"authorization_endpoint": "https://api.fortrabbit.com/oauth2/authorize",
			"token_endpoint":         "https://api.fortrabbit.com/oauth2/token",
			"scopes_supported":       []string{"mcp", "api"},
		}
	})

	_, err := Login(context.Background(), Options{Host: server.URL, DashboardURL: dashboardURL, Out: &strings.Builder{}, Timeout: time.Second,
		OpenBrowser: func(string) error { t.Error("browser opened for a foreign issuer"); return nil }})
	if err == nil || !strings.Contains(err.Error(), "names another issuer") {
		t.Fatalf("Login() error = %v", err)
	}
	if _, err := Refresh(context.Background(), server.URL, server.Client(), "refresh"); err == nil || !strings.Contains(err.Error(), "names another issuer") {
		t.Fatalf("Refresh() error = %v", err)
	}
}

func TestHumanDuration(t *testing.T) {
	for duration, want := range map[time.Duration]string{15 * time.Minute: "15 minutes", time.Minute: "1 minute", 50 * time.Millisecond: "50ms", 90 * time.Second: "1m30s"} {
		if got := humanDuration(duration); got != want {
			t.Errorf("humanDuration(%s) = %q, want %q", duration, got, want)
		}
	}
}

func TestLoginAuthorizeURLCarriesSignupAndNameHint(t *testing.T) {
	cases := []struct {
		name    string
		options Options
		prompt  string
		hint    string
	}{
		{"plain", Options{}, "", ""},
		{"signup", Options{Signup: true}, "create", ""},
		{"name", Options{NameHint: "Ada Lovelace"}, "", "Ada Lovelace"},
		{"both", Options{Signup: true, NameHint: "Ada & Co"}, "create", "Ada & Co"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeServer(t)
			person := &browser{t: t, fake: fake, callbacks: []func(string, string) url.Values{validCallback}, responses: make(chan *http.Response, 1)}
			shown := ""
			options := tc.options
			options.Host, options.DashboardURL, options.OpenBrowser, options.Out, options.Timeout = fake.URL, dashboardURL, person.open, &strings.Builder{}, 5*time.Second
			options.OnURL = func(rawURL string, _ string) error { shown = rawURL; return nil }
			if _, err := Login(context.Background(), options); err != nil {
				t.Fatalf("Login() error = %v", err)
			}
			authorize, err := url.Parse(shown)
			if err != nil {
				t.Fatal(err)
			}
			query := authorize.Query()
			if query.Get("prompt") != tc.prompt || query.Get("name_hint") != tc.hint || query.Has("prompt") != (tc.prompt != "") || query.Has("name_hint") != (tc.hint != "") {
				t.Fatalf("query = %v", query)
			}
		})
	}
}

func TestValidateNameHint(t *testing.T) {
	for _, valid := range []string{"A", strings.Repeat("ä", 100), "Ada Lovelace"} {
		if err := ValidateNameHint(valid); err != nil {
			t.Errorf("ValidateNameHint(%q) = %v", valid, err)
		}
	}
	for _, invalid := range []string{"", strings.Repeat("a", 101), "a\tb", "a\x00b", "a\u0085b"} {
		if err := ValidateNameHint(invalid); err == nil {
			t.Errorf("ValidateNameHint(%q) accepted", invalid)
		}
	}
}

func TestConfirmationChallengeMatchesFRAPI(t *testing.T) {
	// The value FRAPI's OAuthConsentRequest::confirms() computes for this code and challenge.
	if got := confirmationChallenge("WDJB-MJHT", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"); got != "VUDrkPwfVEfHI_VG9IidfbgS1ES30RGRKObkoK748oI" {
		t.Fatalf("confirmationChallenge() = %q", got)
	}
}

func TestConfirmationCodesUseTheAlphabetAndDiffer(t *testing.T) {
	pattern := regexp.MustCompile(`^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`)
	seen := map[string]bool{}
	for range 50 {
		code, err := newConfirmationCode()
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(code) {
			t.Fatalf("code = %q", code)
		}
		seen[code] = true
	}
	if len(seen) < 49 {
		t.Fatalf("only %d distinct codes in 50", len(seen))
	}
}

func TestLoginPrintsTheConfirmationCode(t *testing.T) {
	fake := newFakeServer(t)
	person := &browser{t: t, fake: fake, callbacks: []func(string, string) url.Values{validCallback}, responses: make(chan *http.Response, 1)}
	output := &strings.Builder{}

	if _, err := Login(context.Background(), Options{Host: fake.URL, DashboardURL: dashboardURL, OpenBrowser: person.open, Out: output, Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if !regexp.MustCompile(`Enter this code on the consent page: [A-Z]{4}-[A-Z]{4}`).MatchString(output.String()) {
		t.Fatalf("output = %q", output.String())
	}
}
