package root

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fortrabbit/frbit-cli/internal/cmdutil"
)

type recordedRequest struct {
	method string
	path   string
	body   map[string]any
}

func runAppsUpdate(t *testing.T, tty bool, input string, args ...string) ([]recordedRequest, string, error) {
	t.Helper()
	var recorded []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		recorded = append(recorded, recordedRequest{request.Method, request.URL.Path, body})
		_, _ = writer.Write([]byte(`{"publicId":"ap-abc123","name":"shop","gitRepository":"acme/shop"}`))
	}))
	defer server.Close()
	output := &bytes.Buffer{}
	factory := testFactory(output)
	factory.IOStreams.IsTTY = tty
	factory.IOStreams.In = strings.NewReader(input)
	t.Setenv("FRBIT_TOKEN", "test-token")
	command := NewCmdRoot(factory)
	command.SetErr(output)
	command.SetArgs(append([]string{"--host", server.URL, "apps", "update", "ap-abc123"}, args...))
	err := command.Execute()
	return recorded, output.String(), err
}

func TestAppsUpdateConnectsRepository(t *testing.T) {
	requests, output, err := runAppsUpdate(t, false, "", "--repository", "acme/shop", "--confirm", "ap-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].method != http.MethodPut || requests[0].path != "/v1/apps/ap-abc123/git-repository" || requests[0].body["repository"] != "acme/shop" {
		t.Fatalf("requests = %#v", requests)
	}
	if !strings.Contains(output, "ap-abc123") {
		t.Fatalf("output = %q", output)
	}
}

func TestAppsUpdateRepositoryAndNameInOneCall(t *testing.T) {
	requests, _, err := runAppsUpdate(t, false, "", "--name", "shop", "--repository", "acme/shop", "--confirm", "ap-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].method != http.MethodPatch || requests[0].path != "/v1/apps/ap-abc123" || requests[0].body["name"] != "shop" ||
		requests[1].method != http.MethodPut || requests[1].path != "/v1/apps/ap-abc123/git-repository" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestAppsUpdateGitDeployCommentsFalseIsSent(t *testing.T) {
	requests, _, err := runAppsUpdate(t, false, "", "--git-deploy-comments=false", "--software", "laravel", "--confirm", "ap-abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || requests[0].body["gitDeployComments"] != false || requests[0].body["softwareTemplateName"] != "laravel" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestAppsUpdateTechnicalContacts(t *testing.T) {
	requests, _, err := runAppsUpdate(t, false, "", "--technical-contact", "pn-a1b2c3", "--technical-contact", "pn-d4e5f6")
	if err != nil {
		t.Fatal(err)
	}
	contacts, _ := requests[0].body["technicalContacts"].([]any)
	if len(requests) != 1 || len(contacts) != 2 || contacts[0] != "pn-a1b2c3" {
		t.Fatalf("requests = %#v", requests)
	}
	requests, _, err = runAppsUpdate(t, false, "", "--clear-technical-contacts")
	if err != nil {
		t.Fatal(err)
	}
	contacts, ok := requests[0].body["technicalContacts"].([]any)
	if !ok || len(contacts) != 0 {
		t.Fatalf("clear payload = %#v", requests[0].body)
	}
	if _, _, err = runAppsUpdate(t, false, "", "--clear-technical-contacts", "--technical-contact", "pn-a1b2c3"); err == nil {
		t.Fatal("expected error combining clear and contact")
	}
}

func TestAppsUpdateDisconnectRepositoryNeedsConfirm(t *testing.T) {
	requests, _, err := runAppsUpdate(t, false, "", "--disconnect-repository")
	if err == nil || !strings.Contains(err.Error(), "pass --confirm ap-abc123") || len(requests) != 0 {
		t.Fatalf("err = %v, requests = %#v", err, requests)
	}
	requests, _, err = runAppsUpdate(t, false, "", "--disconnect-repository", "--confirm", "ap-abc123")
	if err != nil || len(requests) != 1 || requests[0].method != http.MethodDelete || requests[0].path != "/v1/apps/ap-abc123/git-repository" {
		t.Fatalf("err = %v, requests = %#v", err, requests)
	}
}

func TestAppsUpdateDisconnectRepositoryPromptsOnTTY(t *testing.T) {
	requests, output, err := runAppsUpdate(t, true, "no\n", "--disconnect-repository")
	if !errors.Is(err, cmdutil.ErrAborted) || len(requests) != 0 || !strings.Contains(output, "stops deployments") || !strings.Contains(output, "Aborted.") {
		t.Fatalf("err = %v, requests = %#v, output = %q", err, requests, output)
	}
}

func TestAppsUpdateRejectsRepositoryWithDisconnect(t *testing.T) {
	if _, _, err := runAppsUpdate(t, false, "", "--repository", "acme/shop", "--disconnect-repository", "--confirm", "ap-abc123"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAppsUpdateReportsPartialFailure(t *testing.T) {
	for _, test := range []struct {
		flag, method, want string
	}{
		{"--repository=acme/shop", http.MethodPut, "app settings updated, but connecting the repository failed"},
		{"--disconnect-repository", http.MethodDelete, "app settings updated, but disconnecting the repository failed"},
	} {
		var methods []string
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			methods = append(methods, request.Method)
			if request.Method == test.method {
				writer.Header().Set("Content-Type", "application/problem+json")
				writer.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = writer.Write([]byte(`{"title":"Validation failed","status":422}`))
				return
			}
			_, _ = writer.Write([]byte(`{"publicId":"ap-abc123"}`))
		}))
		output := &bytes.Buffer{}
		factory := testFactory(output)
		t.Setenv("FRBIT_TOKEN", "test-token")
		command := NewCmdRoot(factory)
		command.SetArgs([]string{"--host", server.URL, "apps", "update", "ap-abc123", "--name", "shop", test.flag, "--confirm", "ap-abc123"})
		err := command.Execute()
		server.Close()
		if err == nil || !strings.Contains(err.Error(), test.want) || len(methods) != 2 || methods[0] != http.MethodPatch {
			t.Fatalf("%s: err = %v, methods = %v", test.flag, err, methods)
		}
	}
}

func TestAppsUpdateWrongConfirmSendsNothing(t *testing.T) {
	requests, _, err := runAppsUpdate(t, false, "", "--disconnect-repository", "--confirm", "ap-wrong1")
	if err == nil || len(requests) != 0 {
		t.Fatalf("err = %v, requests = %#v", err, requests)
	}
}

func TestAppsUpdateConfirmNeedsAGatedFlag(t *testing.T) {
	requests, _, err := runAppsUpdate(t, false, "", "--name", "shop", "--confirm", "ap-abc123")
	if err == nil || !strings.Contains(err.Error(), "only used with --repository, --software or --disconnect-repository") || len(requests) != 0 {
		t.Fatalf("err = %v, requests = %#v", err, requests)
	}
}

func TestAppsUpdateRepositoryAndTemplateNeedConfirm(t *testing.T) {
	for _, args := range [][]string{
		{"--repository", "acme/shop"},
		{"--software", "laravel"},
		{"--name", "shop", "--software", "laravel"},
		{"--repository", "acme/shop", "--confirm", "ap-wrong1"},
	} {
		requests, _, err := runAppsUpdate(t, false, "", args...)
		if err == nil || !strings.Contains(err.Error(), "ap-abc123") || len(requests) != 0 {
			t.Fatalf("%v: err = %v, requests = %#v", args, err, requests)
		}
	}
}

func TestAppsUpdateRepositoryAndTemplatePromptOnceOnTTY(t *testing.T) {
	requests, output, err := runAppsUpdate(t, true, "no\n", "--repository", "acme/shop", "--software", "laravel")
	if !errors.Is(err, cmdutil.ErrAborted) || len(requests) != 0 || !strings.Contains(output, "Aborted.") {
		t.Fatalf("err = %v, requests = %#v, output = %q", err, requests, output)
	}
	for _, want := range []string{"Connecting a repository clears the deploy branch and the deploy file settings of every environment of this app. No environment deploys until a branch is set again.", "Changing the software template sets the software version of every environment of this app to the template default and clears pinned versions. The next deployment uses that version."} {
		if !strings.Contains(output, want) {
			t.Fatalf("output lacks %q: %q", want, output)
		}
	}
	if strings.Count(output, "Type ap-abc123 to confirm:") != 1 {
		t.Fatalf("asked more than once: %q", output)
	}
	requests, _, err = runAppsUpdate(t, true, "ap-abc123\n", "--software", "laravel")
	if err != nil || len(requests) != 1 || requests[0].body["softwareTemplateName"] != "laravel" {
		t.Fatalf("confirmed: err = %v, requests = %#v", err, requests)
	}
}

func TestAppsUpdateFileWithSoftwareTemplateNeedsConfirm(t *testing.T) {
	path := t.TempDir() + "/body.json"
	if err := os.WriteFile(path, []byte(`{"softwareTemplateName":"craft-cms"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	requests, _, err := runAppsUpdate(t, false, "", "--file", path)
	if err == nil || !strings.Contains(err.Error(), "--confirm ap-abc123") || len(requests) != 0 {
		t.Fatalf("err = %v, requests = %#v", err, requests)
	}
	requests, _, err = runAppsUpdate(t, false, "", "--file", path, "--confirm", "ap-abc123")
	if err != nil || len(requests) != 1 || requests[0].body["softwareTemplateName"] != "craft-cms" {
		t.Fatalf("err = %v, requests = %#v", err, requests)
	}
}

func TestAppsUpdateFileWithDeprecatedSoftwarePresetNameNeedsConfirm(t *testing.T) {
	path := t.TempDir() + "/body.json"
	if err := os.WriteFile(path, []byte(`{"softwarePresetName":"craft-cms"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	requests, _, err := runAppsUpdate(t, false, "", "--file", path)
	if err == nil || !strings.Contains(err.Error(), "--confirm ap-abc123") || len(requests) != 0 {
		t.Fatalf("err = %v, requests = %#v", err, requests)
	}
}
