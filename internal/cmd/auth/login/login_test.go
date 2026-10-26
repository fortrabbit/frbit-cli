package login

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/iostreams"
	"github.com/spf13/cobra"
)

type memoryCredentialStore struct {
	credentials map[string]credentials.Credential
}

func (s *memoryCredentialStore) Get(profile string) (credentials.Credential, error) {
	credential, ok := s.credentials[profile]
	if !ok {
		return credentials.Credential{}, credentials.ErrNotFound
	}
	return credential, nil
}
func (s *memoryCredentialStore) Set(profile string, credential credentials.Credential) error {
	s.credentials[profile] = credential
	return nil
}
func (s *memoryCredentialStore) Delete(profile string) error {
	delete(s.credentials, profile)
	return nil
}

type memoryConfigStore struct {
	config config.Config
}

func (s *memoryConfigStore) Load() (config.Config, error) { return s.config, nil }
func (s *memoryConfigStore) Save(value config.Config) error {
	s.config = value
	return nil
}

type fakeAPI struct {
	*httptest.Server
	mu           sync.Mutex
	challenge    string
	redirectURI  string
	authorize    url.Values
	discoveries  int
	bearerTokens []string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	t.Helper()
	fake := &fakeAPI{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			fake.discoveries++
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"issuer":                 fake.URL,
				"authorization_endpoint": fake.URL + "/oauth2/authorize",
				"token_endpoint":         fake.URL + "/oauth2/token",
				"scopes_supported":       []string{"mcp", "api"},
			})
		case "/oauth2/token":
			_ = request.ParseForm()
			sum := sha256.Sum256([]byte(request.PostForm.Get("code_verifier")))
			if request.PostForm.Get("code") != "the-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != fake.challenge || request.PostForm.Get("redirect_uri") != fake.redirectURI {
				writer.WriteHeader(http.StatusBadRequest)
				_, _ = writer.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_, _ = writer.Write([]byte(`{"access_token":"frbit-mcp-at-1","token_type":"Bearer","expires_in":3600,"refresh_token":"frbit-mcp-rt-1"}`))
		case "/v1/you":
			fake.bearerTokens = append(fake.bearerTokens, request.Header.Get("Authorization"))
			_, _ = writer.Write([]byte(`{"publicId":"pe-abc123","email":"ada@example.com","name":"Ada","next":{"step":"connect_git","instruction":"Connect GitHub to deploy from a repository.","url":"https:\/\/dash.example\/git?a=1&b=2"},"optional":[]}`))
		case "/v1/apps":
			fake.bearerTokens = append(fake.bearerTokens, request.Header.Get("Authorization"))
			_, _ = writer.Write([]byte(`[]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

// approve plays the person in the browser: it remembers the PKCE challenge and calls the loopback callback.
func (f *fakeAPI) approve(t *testing.T) func(string) error {
	return func(rawURL string) error {
		authorize, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		query := authorize.Query()
		f.mu.Lock()
		f.challenge = query.Get("code_challenge")
		f.redirectURI = query.Get("redirect_uri")
		f.authorize = query
		f.mu.Unlock()
		go func() {
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			response, err := client.Get(query.Get("redirect_uri") + "?" + url.Values{"code": {"the-code"}, "state": {query.Get("state")}}.Encode())
			if err != nil {
				t.Errorf("callback: %v", err)
				return
			}
			_ = response.Body.Close()
		}()
		return nil
	}
}

func executeLogin(t *testing.T, factory *app.Factory, host string, args ...string) (string, error) {
	t.Helper()
	output := &bytes.Buffer{}
	factory.IOStreams.Out = output
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.AddCommand(NewCmdLogin(factory, nil))
	root.SetOut(output)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append([]string{"--host", host, "login"}, args...))
	err := root.Execute()
	return output.String(), err
}

func loginFactory(t *testing.T, fake *fakeAPI) (*app.Factory, *memoryCredentialStore, *memoryConfigStore) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "")
	t.Setenv("FRBIT_DASHBOARD_URL", "http://localhost:3001")
	store := &memoryCredentialStore{credentials: map[string]credentials.Credential{}}
	configStore := &memoryConfigStore{}
	return &app.Factory{
		IOStreams:          iostreams.IOStreams{In: strings.NewReader("")},
		ConfigStore:        configStore,
		CredentialStore:    store,
		HTTPClient:         fake.Client(),
		Version:            "test",
		OpenBrowser:        fake.approve(t),
		CredentialLockPath: filepath.Join(t.TempDir(), "credentials.lock"),
	}, store, configStore
}

func TestLoginSignsInThroughTheBrowserWithoutATerminal(t *testing.T) {
	fake := newFakeAPI(t)
	factory, store, configStore := loginFactory(t, fake)

	output, err := executeLogin(t, factory, fake.URL)
	if err != nil {
		t.Fatalf("login error = %v", err)
	}
	stored := store.credentials["default"]
	if stored.Kind != credentials.KindOAuth || stored.AccessToken != "frbit-mcp-at-1" || stored.RefreshToken != "frbit-mcp-rt-1" {
		t.Fatalf("stored credential = %#v", stored)
	}
	if configStore.config.Host != fake.URL {
		t.Fatalf("saved host = %q", configStore.config.Host)
	}
	for _, expected := range []string{fake.URL + "/oauth2/authorize?", "ada@example.com", "Connect GitHub to deploy from a repository.", "https://dash.example/git?a=1&b=2"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
	if strings.Index(output, "/oauth2/authorize?") > strings.Index(output, "ada@example.com") {
		t.Fatalf("URL was not printed first: %q", output)
	}
}

func TestLoginJSONPrintsWaitingThenSignedIn(t *testing.T) {
	fake := newFakeAPI(t)
	factory, _, _ := loginFactory(t, fake)

	output, err := executeLogin(t, factory, fake.URL, "--json")
	if err != nil {
		t.Fatalf("login error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 2 {
		t.Fatalf("output lines = %q", lines)
	}
	var waiting struct{ URL, Status string }
	if err := json.Unmarshal([]byte(lines[0]), &waiting); err != nil || waiting.Status != "waiting" || !strings.HasPrefix(waiting.URL, fake.URL+"/oauth2/authorize?") {
		t.Fatalf("first line = %q (%v)", lines[0], err)
	}
	if !strings.HasPrefix(lines[0], `{"url":`) {
		t.Fatalf("first line does not lead with the URL: %q", lines[0])
	}
	var signedIn struct {
		Status string
		Next   struct{ Step, Instruction string }
	}
	if err := json.Unmarshal([]byte(lines[1]), &signedIn); err != nil || signedIn.Status != "signed_in" || signedIn.Next.Step != "connect_git" {
		t.Fatalf("second line = %q (%v)", lines[1], err)
	}
}

func TestLoginTokenStdinKeepsTheTokenPath(t *testing.T) {
	fake := newFakeAPI(t)
	factory, store, _ := loginFactory(t, fake)
	factory.IOStreams.In = strings.NewReader("frbit-at-test\n")
	factory.OpenBrowser = func(string) error {
		t.Error("browser opened for --token-stdin")
		return nil
	}

	output, err := executeLogin(t, factory, fake.URL, "--token-stdin")
	if err != nil {
		t.Fatalf("login error = %v", err)
	}
	if stored := store.credentials["default"]; stored != credentials.APIToken("frbit-at-test") {
		t.Fatalf("stored credential = %#v", stored)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.discoveries != 0 || len(fake.bearerTokens) != 1 || fake.bearerTokens[0] != "Bearer frbit-at-test" {
		t.Fatalf("discoveries = %d, bearer tokens = %q", fake.discoveries, fake.bearerTokens)
	}
	if !strings.Contains(output, "Authenticated profile") {
		t.Fatalf("output = %q", output)
	}
}

func TestLoginRefusesWhileFRBITTokenIsSet(t *testing.T) {
	fake := newFakeAPI(t)
	factory, _, _ := loginFactory(t, fake)
	t.Setenv("FRBIT_TOKEN", "frbit-at-env")
	const message = "FRBIT_TOKEN is set and overrides any stored credential; unset it to sign in in the browser"

	_, err := executeLogin(t, factory, fake.URL)
	if err == nil || err.Error() != message {
		t.Fatalf("login error = %v", err)
	}
	output, err := executeLogin(t, factory, fake.URL, "--json")
	if err == nil || strings.TrimSpace(output) != `{"status":"error","error":"`+message+`"}` {
		t.Fatalf("login --json output = %q, error = %v", output, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.discoveries != 0 {
		t.Fatalf("browser sign-in started with FRBIT_TOKEN set")
	}
}

func TestLoginHelpMentionsTheBrowserAndTokenStdin(t *testing.T) {
	command := NewCmdLogin(&app.Factory{}, nil)
	for _, expected := range []string{"browser", "--token-stdin", "FRBIT_TOKEN"} {
		if !strings.Contains(command.Long, expected) {
			t.Fatalf("help %q does not mention %q", command.Long, expected)
		}
	}
}

func TestLoginJSONReportsTerminalErrorAsJSONLine(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	t.Setenv("FRBIT_TOKEN", "")
	factory := &app.Factory{
		IOStreams:       iostreams.IOStreams{In: strings.NewReader("")},
		ConfigStore:     &memoryConfigStore{},
		CredentialStore: &memoryCredentialStore{credentials: map[string]credentials.Credential{}},
		HTTPClient:      server.Client(),
	}

	output, err := executeLogin(t, factory, server.URL, "--json")
	if err == nil {
		t.Fatal("login succeeded without a sign-in server")
	}
	var line struct{ Status, Error string }
	if decodeErr := json.Unmarshal([]byte(output), &line); decodeErr != nil || line.Status != "error" || line.Error != err.Error() {
		t.Fatalf("output = %q (%v), error = %v", output, decodeErr, err)
	}
}

// urlWriter starts the callback once the JSON waiting line arrives, standing in for a person who copies the URL.
type urlWriter struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	started bool
	onURL   func(string) error
}

func (w *urlWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started {
		var waiting struct{ URL string }
		if json.Unmarshal(bytes.TrimSpace(p), &waiting) == nil && waiting.URL != "" {
			w.started = true
			_ = w.onURL(waiting.URL)
		}
	}
	return w.buffer.Write(p)
}

func TestLoginNoBrowserDoesNotOpenTheBrowser(t *testing.T) {
	fake := newFakeAPI(t)
	factory, store, _ := loginFactory(t, fake)
	factory.OpenBrowser = func(string) error {
		t.Error("browser opened with --no-browser")
		return nil
	}
	writer := &urlWriter{onURL: fake.approve(t)}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.AddCommand(NewCmdLogin(factory, nil))
	root.SetOut(writer)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--host", fake.URL, "login", "--no-browser", "--json"})

	if err := root.Execute(); err != nil {
		t.Fatalf("login error = %v", err)
	}
	if store.credentials["default"].Kind != credentials.KindOAuth {
		t.Fatalf("stored credential = %#v", store.credentials["default"])
	}
}

func TestLoginJSONLinesAreNotEscaped(t *testing.T) {
	fake := newFakeAPI(t)
	factory, _, _ := loginFactory(t, fake)

	output, err := executeLogin(t, factory, fake.URL, "--json")
	if err != nil {
		t.Fatalf("login error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if !strings.Contains(lines[0], "&") || strings.Contains(lines[0], `\u0026`) || strings.Contains(lines[0], `\/`) {
		t.Fatalf("waiting line is escaped: %q", lines[0])
	}
	if !strings.Contains(lines[1], `"url":"https://dash.example/git?a=1&b=2"`) || strings.Contains(lines[1], `\/`) {
		t.Fatalf("signed_in line is escaped: %q", lines[1])
	}
}

func TestLoginSignupAndNameReachTheAuthorizeURL(t *testing.T) {
	for _, extra := range [][]string{{}, {"--json"}, {"--no-browser", "--json"}} {
		fake := newFakeAPI(t)
		factory, _, _ := loginFactory(t, fake)
		args := append([]string{"--signup", "--name", "Ada Lovelace & Co"}, extra...)
		var err error
		if len(extra) > 1 {
			writer := &urlWriter{onURL: fake.approve(t)}
			factory.OpenBrowser = func(string) error { t.Error("browser opened with --no-browser"); return nil }
			root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
			root.PersistentFlags().String("host", "", "")
			root.PersistentFlags().String("profile", app.DefaultProfile, "")
			root.AddCommand(NewCmdLogin(factory, nil))
			root.SetOut(writer)
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(append([]string{"--host", fake.URL, "login"}, args...))
			err = root.Execute()
		} else {
			_, err = executeLogin(t, factory, fake.URL, args...)
		}
		if err != nil {
			t.Fatalf("login %v error = %v", args, err)
		}
		fake.mu.Lock()
		query := fake.authorize
		fake.mu.Unlock()
		if query.Get("prompt") != "create" || query.Get("name_hint") != "Ada Lovelace & Co" {
			t.Fatalf("login %v authorize query = %v", args, query)
		}
	}
}

func TestLoginWithoutSignupSendsNeitherParameter(t *testing.T) {
	fake := newFakeAPI(t)
	factory, _, _ := loginFactory(t, fake)
	if _, err := executeLogin(t, factory, fake.URL); err != nil {
		t.Fatalf("login error = %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.authorize.Has("prompt") || fake.authorize.Has("name_hint") {
		t.Fatalf("authorize query = %v", fake.authorize)
	}
}

func TestLoginRejectsInvalidNameBeforeOpeningTheBrowser(t *testing.T) {
	for _, name := range []string{"", strings.Repeat("a", 101), "Ada\nLovelace", "Ada\x07"} {
		fake := newFakeAPI(t)
		factory, _, _ := loginFactory(t, fake)
		factory.OpenBrowser = func(string) error { t.Error("browser opened for an invalid name"); return nil }
		output, err := executeLogin(t, factory, fake.URL, "--signup", "--name", name, "--json")
		if err == nil || !strings.Contains(err.Error(), "--name") {
			t.Fatalf("name %q: error = %v", name, err)
		}
		if fake.discoveries != 0 || !strings.Contains(output, `"status":"error"`) {
			t.Fatalf("name %q: discoveries = %d, output = %q", name, fake.discoveries, output)
		}
	}
}
