package logout

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/config"
	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/filelock"
	"github.com/spf13/cobra"
)

type memoryStore struct {
	credentials map[string]credentials.Credential
}

func (s *memoryStore) Get(profile string) (credentials.Credential, error) {
	credential, ok := s.credentials[profile]
	if !ok {
		return credentials.Credential{}, credentials.ErrNotFound
	}
	return credential, nil
}
func (s *memoryStore) Set(profile string, credential credentials.Credential) error {
	s.credentials[profile] = credential
	return nil
}
func (s *memoryStore) Delete(profile string) error { delete(s.credentials, profile); return nil }

type configStore struct{}

func (configStore) Load() (config.Config, error) { return config.Config{}, nil }
func (configStore) Save(config.Config) error     { return nil }

type fakeServer struct {
	*httptest.Server
	mu           sync.Mutex
	revokeStatus int
	revokeForms  []url.Values
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fake := &fakeServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"issuer":                 fake.URL,
				"authorization_endpoint": fake.URL + "/oauth2/authorize",
				"token_endpoint":         fake.URL + "/oauth2/token",
				"revocation_endpoint":    fake.URL + "/oauth2/revoke",
				"scopes_supported":       []string{"api"},
			})
		case "/oauth2/revoke":
			_ = request.ParseForm()
			fake.mu.Lock()
			defer fake.mu.Unlock()
			fake.revokeForms = append(fake.revokeForms, request.PostForm)
			if fake.revokeStatus != 0 {
				writer.WriteHeader(fake.revokeStatus)
			}
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (f *fakeServer) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.revokeForms)
}

func execute(t *testing.T, fake *fakeServer, store *memoryStore, lockPath string) (stdout string, stderr string, err error) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	factory := &app.Factory{ConfigStore: configStore{}, CredentialStore: store, HTTPClient: fake.Client(), CredentialLockPath: lockPath}
	root := &cobra.Command{Use: "frbit", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("host", "", "")
	root.PersistentFlags().String("profile", app.DefaultProfile, "")
	root.AddCommand(NewCmdLogout(factory, nil))
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetArgs([]string{"--host", fake.URL, "logout"})
	err = root.Execute()
	return out.String(), errOut.String(), err
}

func oauthStore(fake *fakeServer) *memoryStore {
	return &memoryStore{credentials: map[string]credentials.Credential{
		app.DefaultProfile: {Kind: credentials.KindOAuth, AccessToken: "frbit-at-1", RefreshToken: "frbit-rt-1", Issuer: fake.URL},
	}}
}

func TestLogoutRevokesTheBrowserSignInThenDeletesLocally(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "")
	fake := newFakeServer(t)
	store := oauthStore(fake)

	stdout, stderr, err := execute(t, fake, store, filepath.Join(t.TempDir(), "credentials.lock"))
	if err != nil {
		t.Fatalf("logout error = %v", err)
	}
	if len(store.credentials) != 0 || !strings.Contains(stdout, "Removed stored credentials") || stderr != "" {
		t.Fatalf("stored %v, stdout %q, stderr %q", store.credentials, stdout, stderr)
	}
	form := fake.revokeForms[0]
	if form.Get("token") != "frbit-rt-1" || form.Get("token_type_hint") != "refresh_token" || form.Get("client_id") != fake.URL+"/.well-known/oauth-client/frbit-cli" {
		t.Fatalf("revoke form = %v", form)
	}
	if strings.Contains(stdout+stderr, "frbit-rt-1") || strings.Contains(stdout+stderr, "frbit-at-1") {
		t.Fatalf("output leaks a token: %q %q", stdout, stderr)
	}
}

func TestLogoutDeletesLocallyWhenTheServerFails(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "")
	fake := newFakeServer(t)
	fake.revokeStatus = http.StatusInternalServerError
	store := oauthStore(fake)

	stdout, stderr, err := execute(t, fake, store, filepath.Join(t.TempDir(), "credentials.lock"))
	if err != nil || len(store.credentials) != 0 || !strings.Contains(stdout, "Removed stored credentials") {
		t.Fatalf("error %v, stored %v, stdout %q", err, store.credentials, stdout)
	}
	if !strings.Contains(stderr, "stays listed under Connections in the dashboard until you disconnect it there") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestLogoutDeletesLocallyWhenTheServerIsUnreachable(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "")
	fake := newFakeServer(t)
	store := oauthStore(fake)
	fake.Close()

	_, stderr, err := execute(t, fake, store, filepath.Join(t.TempDir(), "credentials.lock"))
	if err != nil || len(store.credentials) != 0 || !strings.Contains(stderr, "Connections") {
		t.Fatalf("error %v, stored %v, stderr %q", err, store.credentials, stderr)
	}
}

func TestLogoutOfAnAPITokenStaysLocal(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "")
	fake := newFakeServer(t)
	store := &memoryStore{credentials: map[string]credentials.Credential{app.DefaultProfile: credentials.APIToken("frbit-at-api")}}

	_, stderr, err := execute(t, fake, store, filepath.Join(t.TempDir(), "credentials.lock"))
	if err != nil || len(store.credentials) != 0 || stderr != "" || fake.requests() != 0 {
		t.Fatalf("error %v, stored %v, stderr %q, revokes %d", err, store.credentials, stderr, fake.requests())
	}
}

func TestLogoutWarnsWhileFRBITTokenIsSet(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "frbit-at-env")
	fake := newFakeServer(t)
	store := oauthStore(fake)

	stdout, stderr, err := execute(t, fake, store, filepath.Join(t.TempDir(), "credentials.lock"))
	const warning = "FRBIT_TOKEN is set and is used instead of a stored sign-in. Unset it, or revoke the token under API tokens."
	if err != nil || !strings.Contains(stderr, warning) || strings.Contains(stdout, "FRBIT_TOKEN") || strings.Contains(stderr, "frbit-at-env") {
		t.Fatalf("error %v, stdout %q, stderr %q", err, stdout, stderr)
	}
}

func TestLogoutWaitsForTheCredentialLock(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "")
	fake := newFakeServer(t)
	store := oauthStore(fake)
	lockPath := filepath.Join(t.TempDir(), "credentials.lock")
	held, err := filelock.Acquire(t.Context(), lockPath)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, _, err := execute(t, fake, store, lockPath)
		done <- err
	}()
	select {
	case <-done:
		t.Fatal("logout finished while the credential lock was held")
	case <-timeAfterShort():
	}
	if fake.requests() != 0 {
		t.Fatal("logout revoked while the credential lock was held")
	}
	_ = held.Release()
	if err := <-done; err != nil || len(store.credentials) != 0 {
		t.Fatalf("error %v, stored %v", err, store.credentials)
	}
}

func TestLogoutNeedsACredentialLockPath(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "")
	fake := newFakeServer(t)
	store := oauthStore(fake)

	if _, _, err := execute(t, fake, store, ""); err == nil || len(store.credentials) != 1 {
		t.Fatalf("error %v, stored %v", err, store.credentials)
	}
}

func timeAfterShort() <-chan time.Time { return time.After(300 * time.Millisecond) }

func TestLogoutSkipsRevokeForACredentialWithoutIssuer(t *testing.T) {
	t.Setenv("FRBIT_TOKEN", "")
	fake := newFakeServer(t)
	store := &memoryStore{credentials: map[string]credentials.Credential{
		app.DefaultProfile: {Kind: credentials.KindOAuth, AccessToken: "frbit-at-1", RefreshToken: "frbit-rt-1"},
	}}

	_, stderr, err := execute(t, fake, store, filepath.Join(t.TempDir(), "credentials.lock"))
	if err != nil || len(store.credentials) != 0 || fake.requests() != 0 || !strings.Contains(stderr, "stays listed under Connections in the dashboard until you disconnect it there") {
		t.Fatalf("error %v, stored %v, revokes %d, stderr %q", err, store.credentials, fake.requests(), stderr)
	}
}
