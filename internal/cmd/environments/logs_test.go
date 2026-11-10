package environments

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func fastPoll(t *testing.T) {
	logsPollInterval = time.Millisecond
	t.Cleanup(func() { logsPollInterval = 2 * time.Second })
}

func TestLogsFollowPrintsOnlyNewLines(t *testing.T) {
	fastPoll(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	polls := 0
	output, seen, err := runContext(t, ctx, "", func(r request, w http.ResponseWriter) {
		mu.Lock()
		defer mu.Unlock()
		query, _ := url.ParseQuery(r.query)
		switch query.Get("after") {
		case "":
			w.Header().Set("Link", `</v1/environments/en-abc123/logs?source=php&after=c2>; rel="prev"`)
			echo(w, 200, `[{"time":"t2","level":"error","message":"second\n#0 stack\u001b[2J"},{"time":"t1","level":null,"message":"first"}]`)
		case "c2":
			w.Header().Set("Link", `</v1/environments/en-abc123/logs?source=php&after=c3>; rel="prev"`)
			echo(w, 200, `[{"time":"t4","level":"info","message":"fourth"},{"time":"t3","level":"info","message":"third"}]`)
		default:
			polls++
			if polls >= 2 {
				cancel()
			}
			w.Header().Set("Link", `</v1/environments/en-abc123/logs?source=php&after=c3>; rel="prev"`)
			echo(w, 200, `[]`)
		}
	}, "logs", "en-abc123", "--follow")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"t1\t\tfirst\n", "t2\terror\tsecond\n  #0 stack[2J\n", "t3\tinfo\tthird\n", "t4\tinfo\tfourth\n"} {
		if strings.Count(output, want) != 1 {
			t.Fatalf("want %q once in:\n%q", want, output)
		}
	}
	if strings.ContainsRune(output, 0x1b) || strings.Index(output, "first") > strings.Index(output, "third") {
		t.Fatalf("control character or wrong order: %q", output)
	}
	if len(seen) < 3 || !strings.Contains(seen[0].query, "source=php") || !strings.Contains(seen[0].query, "limit=100") {
		t.Fatalf("requests: %+v", seen)
	}
}

func TestLogsFollowExitsZeroOnCancel(t *testing.T) {
	fastPoll(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, err := runContext(t, ctx, "", func(r request, w http.ResponseWriter) {
		if strings.Contains(r.query, "after=") {
			cancel()
		}
		w.Header().Set("Link", `</v1/environments/en-abc123/logs?after=c1>; rel="prev"`)
		echo(w, 200, `[]`)
	}, "logs", "en-abc123", "--follow")
	if err != nil {
		t.Fatalf("cancel must exit cleanly, got %v", err)
	}
}

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

func TestLogsFollowSurvivesTransientError(t *testing.T) {
	fastPoll(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failed := false
	output, _, err := runContext(t, ctx, "", func(r request, w http.ResponseWriter) {
		if !strings.Contains(r.query, "after=") {
			w.Header().Set("Link", `</v1/environments/en-abc123/logs?after=c1>; rel="prev"`)
			echo(w, 200, `[{"time":"t1","level":null,"message":"first"}]`)
			return
		}
		if !failed {
			failed = true
			w.Header().Set("Retry-After", "0")
			echo(w, 503, `{"title":"down"}`)
			return
		}
		cancel()
		echo(w, 200, `[]`)
	}, "logs", "en-abc123", "--follow")
	if err != nil || !failed || strings.Count(output, "first") != 1 {
		t.Fatalf("failed=%v err=%v output=%q", failed, err, output)
	}
	if _, _, err := run(t, "", func(r request, w http.ResponseWriter) { echo(w, 404, `{"title":"nope"}`) }, "logs", "en-abc123"); err == nil {
		t.Fatal("404 must stay fatal")
	}
}

func TestLogsFollowPageWithoutCursorIsNotReprinted(t *testing.T) {
	fastPoll(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	polls := 0
	output, _, err := runContext(t, ctx, "", func(r request, w http.ResponseWriter) {
		if !strings.Contains(r.query, "after=") {
			w.Header().Set("Link", `</v1/environments/en-abc123/logs?after=c1>; rel="prev"`)
			echo(w, 200, `[]`)
			return
		}
		polls++
		switch polls {
		case 1: // entries but no prev link
			echo(w, 200, `[{"time":"t2","level":null,"message":"b"},{"time":"t1","level":null,"message":"a"}]`)
		case 2: // same cursor again, one more entry on top
			echo(w, 200, `[{"time":"t3","level":null,"message":"c"},{"time":"t2","level":null,"message":"b"},{"time":"t1","level":null,"message":"a"}]`)
		default:
			cancel()
			echo(w, 200, `[{"time":"t3","level":null,"message":"c"},{"time":"t2","level":null,"message":"b"},{"time":"t1","level":null,"message":"a"}]`)
		}
	}, "logs", "en-abc123", "--follow")
	if err != nil {
		t.Fatal(err)
	}
	if output != "t1\t\ta\nt2\t\tb\nt3\t\tc\n" {
		t.Fatalf("output %q", output)
	}
}

func TestLogsFollowEmptyLogRequeriesWithoutCursor(t *testing.T) {
	fastPoll(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	output, _, err := runContext(t, ctx, "", func(r request, w http.ResponseWriter) {
		calls++
		switch {
		case calls < 3:
			echo(w, 200, `[]`)
		case calls == 3:
			w.Header().Set("Link", `</v1/environments/en-abc123/logs?after=c1>; rel="prev"`)
			echo(w, 200, `[{"time":"t1","level":null,"message":"hello"}]`)
		default:
			cancel()
			echo(w, 200, `[]`)
		}
	}, "logs", "en-abc123", "--follow")
	if err != nil || output != "t1\t\thello\n" {
		t.Fatalf("err=%v output=%q", err, output)
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
