package environments

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMetricsRangeSetsFromAndTo(t *testing.T) {
	fixed := time.Date(2026, 11, 10, 12, 0, 0, 0, time.FixedZone("CET", 3600))
	metricsNow = func() time.Time { return fixed }
	t.Cleanup(func() { metricsNow = time.Now })
	cases := []struct {
		args       []string
		from, step string
	}{
		{[]string{"--range", "7d"}, "2026-11-03T11:00:00Z", "day"},
		{[]string{"--range", "1h"}, "2026-11-10T10:00:00Z", "hour"},
		{nil, "2026-11-09T11:00:00Z", "hour"},
		{[]string{"--range", "30d", "--step", "hour"}, "2026-10-11T11:00:00Z", "hour"},
	}
	for _, tc := range cases {
		args := append([]string{"metrics", "en-abc123", "php-requests"}, tc.args...)
		_, seen, err := run(t, "", func(r request, w http.ResponseWriter) {
			echo(w, 200, `{"slug":"php-requests","unit":"requests","from":"x","to":"y","step":"day","totals":[{"label":"total","value":4210}],"series":{"total":[["2026-11-01T00:00:00+00:00",120],["2026-11-02T00:00:00+00:00",null]]}}`)
		}, args...)
		if err != nil {
			t.Fatal(err)
		}
		query, _ := url.ParseQuery(seen[0].query)
		if seen[0].path != "/v1/environments/en-abc123/metrics/php-requests" || query.Get("from") != tc.from || query.Get("to") != "2026-11-10T11:00:00Z" || query.Get("step") != tc.step {
			t.Fatalf("%v: %s?%s", tc.args, seen[0].path, seen[0].query)
		}
	}
	if _, _, err := run(t, "", func(request, http.ResponseWriter) {}, "metrics", "en-abc123", "x", "--range", "2d"); err == nil {
		t.Fatal("bad range accepted")
	}
}

func TestMetricsSummaryTable(t *testing.T) {
	output, _, err := run(t, "", func(r request, w http.ResponseWriter) {
		if r.path != "/v1/environments/en-abc123/metrics" {
			t.Fatalf("path %s", r.path)
		}
		echo(w, 200, `{"usage":[{"component":"storage","metric":"web-storage","current":268435456,"limit":536870912,"unit":"bytes","percent":50,"period":"now"},{"component":"traffic","metric":"traffic","current":null,"limit":null,"unit":"bytes","percent":null,"period":"month"}],"last24Hours":{"requests":12840,"errors":{"500":3,"503":0,"504":null},"responseTimeMs":{"p99":840,"median":62},"phpMemoryMaxPercent":71.4,"outOfMemoryKills":{"php":0,"jobs":null}},"metrics":["php-requests","web-storage"]}`)
	}, "metrics", "en-abc123")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"COMPONENT", "storage", "256.0 MB", "512.0 MB", "50.0%", "12840", "Last 24 hours", "HTTP 504                    -", "71.4%", "(this month)", "traffic", "php-requests, web-storage"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in:\n%s", want, output)
		}
	}
}

func TestMetricsHistoryOutput(t *testing.T) {
	output, _, err := run(t, "", func(r request, w http.ResponseWriter) {
		echo(w, 200, `{"slug":"php-requests","unit":"requests","from":"a","to":"b","step":"day","totals":[{"label":"total","value":4210}],"series":{"total":[["2026-11-01T00:00:00+00:00",120],["2026-11-02T00:00:00+00:00",null]]}}`)
	}, "metrics", "en-abc123", "php-requests")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"php-requests (requests)", "total  4210", "TIME", "2026-11-01T00:00:00+00:00  120", "2026-11-02T00:00:00+00:00  -"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in:\n%s", want, output)
		}
	}
}
