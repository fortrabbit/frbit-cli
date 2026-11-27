package root

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testEd25519Key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIHILxiqSWDJ26SSjVOTA8LukzKKP618w1Okm/1v8/2p lusine@laptop"
	testRSAKey     = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAQQCo9+BpMRYQ/dL3DS2CyJxRF+j6ctbT3/Qp84+KeFhnii7NT7fELilKUSnxS30WAvQCCo2yU1orfgqr41mM70MB lusine@old"
)

// sshHome makes a home directory holding the given files below .ssh.
func sshHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(home, ".ssh", name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// keyServer answers the ssh-keys list with existing and records what add posts.
func keyServer(t *testing.T, existing string, posted *map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/ssh-keys":
			_, _ = w.Write([]byte(existing))
		case "POST /v1/ssh-keys":
			_ = json.NewDecoder(r.Body).Decode(posted)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"publicId":"sk-abc123","comment":"lusine@laptop","type":"ed25519","fingerprint":"9c:2f","publicKey":"AAAA","createdAt":"2026-11-27T10:00:00+00:00"}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL)
		}
	}
}

func TestSSHKeysListPrintsTheKeys(t *testing.T) {
	output, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ssh-keys" {
			t.Fatalf("unexpected %s", r.URL)
		}
		_, _ = w.Write([]byte(`[{"publicId":"sk-abc123","comment":"lusine@laptop","type":"ed25519","fingerprint":"9c:2f:7a","publicKey":"AAAA","createdAt":"2026-11-27T10:00:00+00:00"}]`))
	}, "ssh-keys", "list")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sk-abc123", "lusine@laptop", "ed25519", "9c:2f:7a"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in:\n%s", want, output)
		}
	}
}

func TestSSHKeysAddSendsTheGivenPublicKeyLine(t *testing.T) {
	home := sshHome(t, map[string]string{"work.pub": testEd25519Key + "\n"})
	var posted map[string]any
	output, err := runDomains(t, keyServer(t, `[]`, &posted), "ssh-keys", "add", filepath.Join(home, ".ssh", "work.pub"))
	if err != nil {
		t.Fatal(err)
	}
	if posted["sshKey"] != testEd25519Key || len(posted) != 1 {
		t.Fatalf("posted = %#v", posted)
	}
	if !strings.Contains(output, "Added SSH key sk-abc123") || !strings.Contains(output, "9c:2f") {
		t.Fatalf("output:\n%s", output)
	}
}

func TestSSHKeysAddPrefersEd25519ThenRSA(t *testing.T) {
	sshHome(t, map[string]string{"id_ed25519.pub": testEd25519Key, "id_rsa.pub": testRSAKey})
	var posted map[string]any
	if _, err := runDomains(t, keyServer(t, `[]`, &posted), "ssh-keys", "add"); err != nil {
		t.Fatal(err)
	}
	if posted["sshKey"] != testEd25519Key {
		t.Fatalf("posted = %#v", posted)
	}

	sshHome(t, map[string]string{"id_rsa.pub": testRSAKey})
	posted = nil
	if _, err := runDomains(t, keyServer(t, `[]`, &posted), "ssh-keys", "add"); err != nil {
		t.Fatal(err)
	}
	if posted["sshKey"] != testRSAKey {
		t.Fatalf("posted = %#v", posted)
	}
}

func TestSSHKeysAddWithoutAKeyNamesSSHKeygen(t *testing.T) {
	sshHome(t, nil)
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "ssh-keys", "add")
	if err == nil || !strings.Contains(err.Error(), "ssh-keygen -t ed25519") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHKeysAddRefusesAPrivateKey(t *testing.T) {
	home := sshHome(t, map[string]string{
		"id_ed25519":     "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ\n-----END OPENSSH PRIVATE KEY-----\n",
		"id_ed25519.pub": testEd25519Key,
	})
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "ssh-keys", "add", filepath.Join(home, ".ssh", "id_ed25519"))
	if err == nil || !strings.Contains(err.Error(), "is a private key") || !strings.Contains(err.Error(), "id_ed25519.pub") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHKeysAddRefusesAFileThatIsNotOnePublicKey(t *testing.T) {
	home := sshHome(t, map[string]string{"two.pub": testEd25519Key + "\n" + testRSAKey + "\n", "notes.pub": "hello world\n"})
	for _, name := range []string{"two.pub", "notes.pub"} {
		_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "ssh-keys", "add", filepath.Join(home, ".ssh", name))
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
	}
}

func TestSSHKeysAddCountsAKeyOnTheAccountAsDone(t *testing.T) {
	sshHome(t, map[string]string{"id_ed25519.pub": testEd25519Key})
	var posted map[string]any
	output, err := runDomains(t, keyServer(t, `[{"publicId":"sk-old123","publicKey":"AAAAC3NzaC1lZDI1NTE5AAAAIIHILxiqSWDJ26SSjVOTA8LukzKKP618w1Okm/1v8/2p"}]`, &posted), "ssh-keys", "add")
	if err != nil {
		t.Fatal(err)
	}
	if posted != nil || !strings.Contains(output, "already on your account (sk-old123)") {
		t.Fatalf("posted = %#v\n%s", posted, output)
	}

	output, err = runDomains(t, keyServer(t, `[{"publicId":"sk-old123","publicKey":"AAAAC3NzaC1lZDI1NTE5AAAAIIHILxiqSWDJ26SSjVOTA8LukzKKP618w1Okm/1v8/2p"}]`, &posted), "ssh-keys", "add", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var printed map[string]any
	if err := json.Unmarshal([]byte(output), &printed); err != nil || printed["publicId"] != "sk-old123" || posted != nil {
		t.Fatalf("err = %v, posted = %#v\n%s", err, posted, output)
	}
}

func TestSSHKeysAddShowsTheRefusalOfAnAPIToken(t *testing.T) {
	sshHome(t, map[string]string{"id_ed25519.pub": testEd25519Key})
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"type":"about:blank","title":"Forbidden","status":403,"detail":"Only a frbit CLI login can add SSH keys: sign in with \"frbit auth login\" and run \"frbit ssh-keys add\", or add the key in the dashboard: https://dash.example/credentials/ssh-keys"}`))
	}, "ssh-keys", "add")
	if err == nil || !strings.Contains(err.Error(), "Only a frbit CLI login can add SSH keys") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHKeysDeleteNeedsConfirm(t *testing.T) {
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "ssh-keys", "delete", "sk-abc123")
	if err == nil || !strings.Contains(err.Error(), "--confirm sk-abc123") {
		t.Fatalf("err = %v", err)
	}
	var calls []string
	output, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"publicId":"sk-abc123","comment":"lusine@laptop","fingerprint":"9c:2f"}`))
	}, "ssh-keys", "delete", "sk-abc123", "--confirm", "sk-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "GET /v1/ssh-keys/sk-abc123,DELETE /v1/ssh-keys/sk-abc123" || !strings.Contains(output, "Deleted SSH key sk-abc123.") {
		t.Fatalf("calls = %v\n%s", calls, output)
	}
}

func TestSSHKeysAddRefusesKeysTheAPIDoesNotAccept(t *testing.T) {
	home := sshHome(t, map[string]string{
		"ecdsa.pub":    "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTY= me@laptop\n",
		"options.pub":  `from="10.0.0.1" ` + testEd25519Key + "\n",
		"mismatch.pub": "ssh-rsa AAAAC3NzaC1lZDI1NTE5AAAAIIHILxiqSWDJ26SSjVOTA8LukzKKP618w1Okm/1v8/2p me@laptop\n",
		"control.pub":  testEd25519Key + "\x1b[2J\n",
	})
	for name, want := range map[string]string{
		"ecdsa.pub":    "accepts ed25519 and rsa keys only",
		"options.pub":  "remove everything before ssh-ed25519",
		"mismatch.pub": "does not match its type",
		"control.pub":  "control characters",
	} {
		_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("%s: unexpected request", name) }, "ssh-keys", "add", filepath.Join(home, ".ssh", name))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestSSHKeysAddRefusesADevice(t *testing.T) {
	sshHome(t, nil)
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "ssh-keys", "add", os.DevNull)
	if err == nil || !strings.Contains(err.Error(), "is not a public key file") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHKeysAddStripsABOMAndCRLF(t *testing.T) {
	home := sshHome(t, map[string]string{"win.pub": "\xEF\xBB\xBF" + testEd25519Key + "\r\n"})
	var posted map[string]any
	if _, err := runDomains(t, keyServer(t, `[]`, &posted), "ssh-keys", "add", filepath.Join(home, ".ssh", "win.pub")); err != nil {
		t.Fatal(err)
	}
	if posted["sshKey"] != testEd25519Key {
		t.Fatalf("posted = %#v", posted)
	}
}

func TestSSHKeysAddExpandsAQuotedTilde(t *testing.T) {
	sshHome(t, map[string]string{"work.pub": testEd25519Key})
	var posted map[string]any
	if _, err := runDomains(t, keyServer(t, `[]`, &posted), "ssh-keys", "add", "~/.ssh/work.pub"); err != nil {
		t.Fatal(err)
	}
	if posted["sshKey"] != testEd25519Key {
		t.Fatalf("posted = %#v", posted)
	}
}

func TestSSHKeysAddSkipsADefaultPathThatIsNoFile(t *testing.T) {
	home := sshHome(t, map[string]string{"id_rsa.pub": testRSAKey})
	if err := os.Mkdir(filepath.Join(home, ".ssh", "id_ed25519.pub"), 0o700); err != nil {
		t.Fatal(err)
	}
	var posted map[string]any
	if _, err := runDomains(t, keyServer(t, `[]`, &posted), "ssh-keys", "add"); err != nil {
		t.Fatal(err)
	}
	if posted["sshKey"] != testRSAKey {
		t.Fatalf("posted = %#v", posted)
	}
}

func TestSSHKeysAddWithoutHomeAsksForTheFile(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "ssh-keys", "add")
	if err == nil || !strings.Contains(err.Error(), "pass the public key file") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHKeysAddPrintsTheCreatedKeyAsJSON(t *testing.T) {
	sshHome(t, map[string]string{"id_ed25519.pub": testEd25519Key})
	var posted map[string]any
	output, err := runDomains(t, keyServer(t, `[]`, &posted), "ssh-keys", "add", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var printed map[string]any
	if err := json.Unmarshal([]byte(output), &printed); err != nil || printed["publicId"] != "sk-abc123" {
		t.Fatalf("err = %v\n%s", err, output)
	}
}

func TestSSHKeysAddShowsAConflictWithADeployKey(t *testing.T) {
	sshHome(t, map[string]string{"id_ed25519.pub": testEd25519Key})
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"title":"Unprocessable Content","status":422,"detail":"sshKey: Environment en-abc123 already has this SSH key as a deploy key.","errors":{"sshKey":"Environment en-abc123 already has this SSH key as a deploy key."}}`))
	}, "ssh-keys", "add")
	if err == nil || !strings.Contains(err.Error(), "already has this SSH key as a deploy key") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHKeysDeleteOfAKeyWithoutCommentNamesTheFingerprint(t *testing.T) {
	var stderr bytes.Buffer
	_, err := runDomainsWithStderr(t, &stderr, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = w.Write([]byte(`{"publicId":"sk-abc123","comment":"","fingerprint":"9c:2f"}`))
	}, "ssh-keys", "delete", "sk-abc123", "--confirm", "sk-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "SSH key: 9c:2f (sk-abc123)") {
		t.Fatalf("stderr:\n%s", stderr.String())
	}
}

func runDomainsWithStderr(t *testing.T, stderr *bytes.Buffer, handler http.HandlerFunc, args ...string) (string, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	t.Setenv("FRBIT_TOKEN", "test-token")
	output := &bytes.Buffer{}
	factory := testFactory(output)
	factory.IOStreams.ErrOut = stderr
	command := NewCmdRoot(factory)
	command.SetErr(stderr)
	command.SetArgs(append([]string{"--host", server.URL}, args...))
	err := command.Execute()
	return output.String(), err
}

func TestSSHKeysAddNamesTheConversionOfAnSSH2File(t *testing.T) {
	home := sshHome(t, map[string]string{"putty.pub": "---- BEGIN SSH2 PUBLIC KEY ----\nComment: \"lusine\"\nAAAAC3NzaC1lZDI1NTE5AAAAIIHILxiqSWDJ26SSjVOTA8LukzKKP618w1Okm/1v8/2p\n---- END SSH2 PUBLIC KEY ----\n"})
	_, err := runDomains(t, func(w http.ResponseWriter, r *http.Request) { t.Fatalf("unexpected request") }, "ssh-keys", "add", filepath.Join(home, ".ssh", "putty.pub"))
	if err == nil || !strings.Contains(err.Error(), "ssh-keygen -i -f") {
		t.Fatalf("err = %v", err)
	}
}

func TestSSHKeysAddAcceptsATabBetweenFields(t *testing.T) {
	line := strings.Replace(testEd25519Key, " ", "\t", 1)
	home := sshHome(t, map[string]string{"tab.pub": line + "\n"})
	var posted map[string]any
	if _, err := runDomains(t, keyServer(t, `[]`, &posted), "ssh-keys", "add", filepath.Join(home, ".ssh", "tab.pub")); err != nil {
		t.Fatal(err)
	}
	if posted["sshKey"] != line {
		t.Fatalf("posted = %#v", posted)
	}
}
