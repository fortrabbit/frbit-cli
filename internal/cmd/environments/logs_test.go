package environments

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLogsPagesBackAndFilters(t *testing.T) {
	output, seen, err := run(t, "", func(r request, w http.ResponseWriter) {
		if !strings.Contains(r.query, "before=") {
			w.Header().Set("Link", `</v1/environments/en-abc123/logs?source=access&status=5xx&before=b1>; rel="next"`)
			echo(w, 200, `[{"time":"t3","level":null,"message":"GET /c 500"},{"time":"t2","level":null,"message":"GET /b 502"}]`)
			return
		}
		echo(w, 200, `[{"time":"t1","level":null,"message":"GET /a 500"}]`)
	}, "logs", "en-abc123", "--source", "access", "--status", "5xx", "--method", "get", "--limit", "3", "--since", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !strings.Contains(seen[0].query, "method=GET") || !strings.Contains(seen[0].query, "since=1h") || !strings.Contains(seen[0].query, "status=5xx") {
		t.Fatalf("requests: %+v", seen)
	}
	if !strings.HasPrefix(output, "t1\t") || !strings.Contains(output, "t3\t\tGET /c 500") {
		t.Fatalf("output: %q", output)
	}
	if _, _, err := run(t, "", func(request, http.ResponseWriter) {}, "logs", "en-abc123", "--status", "5xx"); err == nil {
		t.Fatal("--status accepted for php")
	}
}

func TestLogsJSONRawForOnePageMergedForMany(t *testing.T) {
	one, _, err := run(t, "", func(r request, w http.ResponseWriter) {
		echo(w, 200, `[{"time":"t1","level":null,"message":"a","extra":1}]`)
	}, "logs", "en-abc123", "--json")
	if err != nil || strings.TrimSpace(one) != `[{"time":"t1","level":null,"message":"a","extra":1}]` {
		t.Fatalf("err=%v %q", err, one)
	}
	many, _, err := run(t, "", func(r request, w http.ResponseWriter) {
		if !strings.Contains(r.query, "before=") {
			w.Header().Set("Link", `</v1/environments/en-abc123/logs?before=b1>; rel="next"`)
			echo(w, 200, `[{"time":"t2","level":null,"message":"b"}]`)
			return
		}
		echo(w, 200, `[{"time":"t1","level":null,"message":"a"}]`)
	}, "logs", "en-abc123", "--json")
	if err != nil || !strings.HasPrefix(many, `[{"time":"t2"`) || !strings.Contains(many, `{"time":"t1"`) {
		t.Fatalf("err=%v %q", err, many)
	}
}

func TestLogsKeepTabsStripsOtherControl(t *testing.T) {
	output, _, err := run(t, "", func(r request, w http.ResponseWriter) {
		echo(w, 200, `[{"time":"t1","level":null,"message":"a\tb\u0007c"}]`)
	}, "logs", "en-abc123")
	if err != nil || !strings.Contains(output, "a\tbc") {
		t.Fatalf("err=%v %q", err, output)
	}
}

func TestLogsFollowPrintsOnlyNewEntriesUntilCancelled(t *testing.T) {
	previous := FollowInterval
	FollowInterval = 5 * time.Millisecond
	t.Cleanup(func() { FollowInterval = previous })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	output, seen, err := runContext(t, ctx, "", func(r request, w http.ResponseWriter) {
		calls++
		switch calls {
		case 1:
			echo(w, 200, `[{"time":"t2","level":null,"message":"b"},{"time":"t1","level":null,"message":"a"}]`)
		case 2:
			// since=t2 returns the boundary entry again, plus one new at the same time and one later.
			echo(w, 200, `[{"time":"t3","level":null,"message":"d"},{"time":"t2","level":null,"message":"c"},{"time":"t2","level":null,"message":"b"}]`)
		default:
			cancel()
			echo(w, 200, `[]`)
		}
	}, "logs", "en-abc123", "--follow", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) < 2 || !strings.Contains(seen[1].query, "since=t2") {
		t.Fatalf("requests: %+v", seen)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var messages []string
	for _, line := range lines {
		var entry struct{ Message string }
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		messages = append(messages, entry.Message)
	}
	if strings.Join(messages, ",") != "a,b,c,d" {
		t.Fatalf("messages = %v\noutput:\n%s", messages, output)
	}
}
