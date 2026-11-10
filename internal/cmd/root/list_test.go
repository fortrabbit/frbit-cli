package root

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// pagedAppsServer serves 5 apps in pages of 2, linking each page to the next.
func pagedAppsServer(t *testing.T, requests *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		*requests = append(*requests, request.URL.RawQuery)
		page, _ := strconv.Atoi(request.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		var items []string
		for index := (page-1)*2 + 1; index <= 5 && index <= page*2; index++ {
			items = append(items, fmt.Sprintf(`{"publicId":"ap-00000%d","name":"App %d"}`, index, index))
		}
		if page*2 < 5 {
			writer.Header().Set("Link", fmt.Sprintf(`</v1/apps?page=%d>; rel="next"`, page+1))
		}
		writer.Header().Set("X-Total-Count", "5")
		_, _ = writer.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	t.Cleanup(server.Close)
	return server
}

func runRoot(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	t.Setenv("FRBIT_TOKEN", "test-token")
	output := &bytes.Buffer{}
	factory := testFactory(output)
	code := Execute(factory, args)
	return output.String(), factory.IOStreams.ErrOut.(*bytes.Buffer).String(), code
}

func TestListAllFetchesEveryPage(t *testing.T) {
	var requests []string
	server := pagedAppsServer(t, &requests)

	stdout, stderr, code := runRoot(t, "--host", server.URL, "apps", "list", "--all")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if len(requests) != 3 {
		t.Fatalf("requests = %v, want 3", requests)
	}
	for _, expected := range []string{"ap-000001", "ap-000005", "Total: 5 apps"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("output %q does not contain %q", stdout, expected)
		}
	}
}

func TestListLimitStopsEarlyAndPrintsJSONArray(t *testing.T) {
	var requests []string
	server := pagedAppsServer(t, &requests)

	stdout, stderr, code := runRoot(t, "--host", server.URL, "environments", "list", "--limit", "3", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %v, want 2", requests)
	}
	if got := strings.TrimSpace(stdout); got != `[{"name":"App 1","publicId":"ap-000001"},{"name":"App 2","publicId":"ap-000002"},{"name":"App 3","publicId":"ap-000003"}]` {
		t.Fatalf("json = %q", got)
	}
}

func TestListAllWithPageIsAUsageError(t *testing.T) {
	_, stderr, code := runRoot(t, "--host", "http://127.0.0.1:1", "apps", "list", "--all", "--page", "2")
	if code == 0 || !strings.Contains(stderr, "--all") || !strings.Contains(stderr, "--page") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestListLimitMustBePositive(t *testing.T) {
	_, stderr, code := runRoot(t, "--host", "http://127.0.0.1:1", "teams", "list", "--limit", "0")
	if code == 0 || !strings.Contains(stderr, "--limit") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestEveryListHasLimitAndAll(t *testing.T) {
	command := NewCmdRoot(testFactory(&bytes.Buffer{}))
	for _, group := range []string{"apps", "environments", "deployments", "domains", "people", "teams", "payment-methods"} {
		list, _, err := command.Find([]string{group, "list"})
		if err != nil {
			t.Fatal(err)
		}
		if list.Flags().Lookup("limit") == nil || list.Flags().Lookup("all") == nil {
			t.Fatalf("%s list lacks --limit or --all", group)
		}
	}
}

func TestJSONErrorPrintsProblemDocument(t *testing.T) {
	const problem = `{"type":"about:blank","title":"Unprocessable Content","status":422,"detail":"name: too short","violations":[{"propertyPath":"name","message":"too short"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/problem+json")
		writer.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = writer.Write([]byte(problem))
	}))
	defer server.Close()

	stdout, _, code := runRoot(t, "--host", server.URL, "apps", "create", "--name", "x", "--region", "eu", "--json")
	if code == 0 {
		t.Fatal("exit = 0, want non-zero")
	}
	if strings.TrimSpace(stdout) != problem {
		t.Fatalf("stdout = %q, want the problem document", stdout)
	}

	stdout, stderr, code := runRoot(t, "--host", server.URL, "apps", "create", "--name", "x", "--region", "eu")
	if code == 0 || stdout != "" || !strings.Contains(stderr, "name: too short") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestAPIWarningsGoToCommandStderr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Deprecation", "@1767225600")
		_, _ = writer.Write([]byte(`{"publicId":"ap-abc123"}`))
	}))
	defer server.Close()

	stdout, stderr, code := runRoot(t, "--host", server.URL, "apps", "get", "ap-abc123", "--json")
	if code != 0 || strings.Contains(stdout, "warning") || !strings.Contains(stderr, "warning: @1767225600") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}
