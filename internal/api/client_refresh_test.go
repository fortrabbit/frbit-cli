package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortrabbit/frbit-cli/internal/credentials"
	"github.com/fortrabbit/frbit-cli/internal/oauth"
)

type memoryStore struct {
	mu          sync.Mutex
	credentials map[string]credentials.Credential
}

func (s *memoryStore) Get(profile string) (credentials.Credential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	credential, ok := s.credentials[profile]
	if !ok {
		return credentials.Credential{}, credentials.ErrNotFound
	}
	return credential, nil
}

func (s *memoryStore) Set(profile string, credential credentials.Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentials[profile] = credential
	return nil
}

func (s *memoryStore) Delete(profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.credentials, profile)
	return nil
}

// rotatingServer issues rotating refresh tokens and, like FRAPI, revokes the whole grant when a consumed one returns.
type rotatingServer struct {
	*httptest.Server
	mu            sync.Mutex
	generation    int
	validAccess   map[string]bool
	refreshToken  string
	consumed      map[string]bool
	refreshCalls  int
	familyRevoked bool
}

func newRotatingServer(t *testing.T, access string, refresh string) *rotatingServer {
	t.Helper()
	server := &rotatingServer{validAccess: map[string]bool{access: true}, refreshToken: refresh, consumed: map[string]bool{}}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serve))
	t.Cleanup(server.Close)
	return server
}

func (s *rotatingServer) serve(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/.well-known/oauth-authorization-server":
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"issuer":                 s.URL,
			"authorization_endpoint": s.URL + "/oauth2/authorize",
			"token_endpoint":         s.URL + "/oauth2/token",
			"scopes_supported":       []string{"mcp", "api"},
		})
	case "/oauth2/token":
		_ = request.ParseForm()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.refreshCalls++
		presented := request.PostForm.Get("refresh_token")
		if s.consumed[presented] {
			s.familyRevoked = true
			s.validAccess = map[string]bool{}
			s.refreshToken = ""
		}
		if s.familyRevoked || presented != s.refreshToken {
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		s.consumed[presented] = true
		s.generation++
		access := fmt.Sprintf("access-%d", s.generation+1)
		s.refreshToken = fmt.Sprintf("refresh-%d", s.generation+1)
		s.validAccess = map[string]bool{access: true}
		_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600, "refresh_token": s.refreshToken})
	case "/v1/apps":
		s.mu.Lock()
		valid := s.validAccess[strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")]
		s.mu.Unlock()
		if !valid {
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"error":"invalid_token"}`))
			return
		}
		_, _ = writer.Write([]byte(`[]`))
	default:
		http.NotFound(writer, request)
	}
}

func (s *rotatingServer) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshCalls
}

func (s *rotatingServer) refresh(ctx context.Context, refreshToken string) (credentials.Credential, error) {
	return oauth.Refresh(ctx, s.URL, s.Client(), refreshToken)
}

func newOAuthClient(t *testing.T, server *rotatingServer, store credentials.Store, lockPath string) *Client {
	t.Helper()
	current, err := store.Get("default")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClientWithTokens(server.URL, NewOAuthTokens(server.URL, store, "default", lockPath, current, server.refresh), server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestConcurrentRefreshesShareOneRotation(t *testing.T) {
	server := newRotatingServer(t, "access-1", "refresh-1")
	store := &memoryStore{credentials: map[string]credentials.Credential{
		"default": {Kind: credentials.KindOAuth, AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(-time.Minute), Issuer: server.URL},
	}}
	lockPath := filepath.Join(t.TempDir(), "credentials.lock")
	// Each client stands for a separate frbit process: own in-memory credential, shared store and lock file.
	clients := []*Client{newOAuthClient(t, server, store, lockPath), newOAuthClient(t, server, store, lockPath)}

	var wait sync.WaitGroup
	errs := make([]error, len(clients))
	for index, client := range clients {
		wait.Go(func() {
			_, errs[index] = client.Apps(context.Background(), 1)
		})
	}
	wait.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("client %d: %v", index, err)
		}
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.refreshCalls != 1 || server.familyRevoked {
		t.Fatalf("refresh calls = %d, family revoked = %t", server.refreshCalls, server.familyRevoked)
	}
	stored, _ := store.Get("default")
	if stored.AccessToken != "access-2" || stored.RefreshToken != "refresh-2" {
		t.Fatalf("stored credential = %#v", stored)
	}
}

func TestRefreshesOnceAfterUnauthorized(t *testing.T) {
	server := newRotatingServer(t, "access-0", "refresh-1")
	store := &memoryStore{credentials: map[string]credentials.Credential{
		"default": {Kind: credentials.KindOAuth, AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(time.Hour), Issuer: server.URL},
	}}
	client := newOAuthClient(t, server, store, filepath.Join(t.TempDir(), "credentials.lock"))

	if _, err := client.Apps(context.Background(), 1); err != nil {
		t.Fatalf("Apps() error = %v", err)
	}
	if calls := server.calls(); calls != 1 {
		t.Fatalf("refresh calls = %d", calls)
	}
	if stored, _ := store.Get("default"); stored.AccessToken != "access-2" {
		t.Fatalf("stored credential = %#v", stored)
	}
}

func TestRefreshesBeforeExpiry(t *testing.T) {
	server := newRotatingServer(t, "access-1", "refresh-1")
	store := &memoryStore{credentials: map[string]credentials.Credential{
		"default": {Kind: credentials.KindOAuth, AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(30 * time.Second), Issuer: server.URL},
	}}
	client := newOAuthClient(t, server, store, filepath.Join(t.TempDir(), "credentials.lock"))

	if _, err := client.Apps(context.Background(), 1); err != nil {
		t.Fatalf("Apps() error = %v", err)
	}
	if calls := server.calls(); calls != 1 {
		t.Fatalf("refresh calls = %d", calls)
	}
}

func TestRevokedGrantAsksToSignInAgain(t *testing.T) {
	server := newRotatingServer(t, "access-0", "refresh-other")
	store := &memoryStore{credentials: map[string]credentials.Credential{
		"default": {Kind: credentials.KindOAuth, AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(-time.Minute), Issuer: server.URL},
	}}
	client := newOAuthClient(t, server, store, filepath.Join(t.TempDir(), "credentials.lock"))

	_, err := client.Apps(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "frbit auth login") {
		t.Fatalf("Apps() error = %v", err)
	}
}

func TestStaticTokenIsNotRetriedAfterUnauthorized(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "frbit-at-test", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.Apps(context.Background(), 1)
	if httpErr, ok := err.(*HTTPError); !ok || httpErr.Status != http.StatusUnauthorized || calls != 1 {
		t.Fatalf("Apps() error = %v, calls = %d", err, calls)
	}
}

func TestRefreshRefusesACredentialFromAnotherIssuer(t *testing.T) {
	server := newRotatingServer(t, "access-1", "refresh-1")
	store := &memoryStore{credentials: map[string]credentials.Credential{
		"default": {Kind: credentials.KindOAuth, AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(-time.Minute), Issuer: server.URL},
	}}
	client := newOAuthClient(t, server, store, filepath.Join(t.TempDir(), "credentials.lock"))
	// Another process signs the profile in to a different host after this one started.
	_ = store.Set("default", credentials.Credential{Kind: credentials.KindOAuth, AccessToken: "other", RefreshToken: "other-refresh", ExpiresAt: time.Now().Add(-time.Minute), Issuer: "https://api.fortrabbit.com"})

	_, err := client.Apps(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "run `frbit auth login` for that host") {
		t.Fatalf("Apps() error = %v", err)
	}
	if calls := server.calls(); calls != 0 {
		t.Fatalf("refresh calls = %d", calls)
	}
}
