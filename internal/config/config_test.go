package config

import "testing"

func TestAPITokensURL(t *testing.T) {
	tests := []struct {
		name      string
		dashboard string
		want      string
	}{
		{name: "production default", want: "https://dash.fortrabbit.com/you/api-tokens"},
		{name: "development override", dashboard: "http://localhost:3001", want: "http://localhost:3001/you/api-tokens"},
		{name: "trailing slash", dashboard: "http://localhost:3001/", want: "http://localhost:3001/you/api-tokens"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("FRBIT_DASHBOARD_URL", test.dashboard)
			got, err := APITokensURL()
			if err != nil {
				t.Fatalf("APITokensURL() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("APITokensURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAPITokensURLRejectsInvalidOverride(t *testing.T) {
	tests := []string{
		"localhost:3001",
		"file:///tmp/token",
		"https://dashboard.example.com/path",
		"https://dashboard.example.com?next=/you/api-tokens",
	}

	for _, dashboardURL := range tests {
		t.Run(dashboardURL, func(t *testing.T) {
			t.Setenv("FRBIT_DASHBOARD_URL", dashboardURL)
			if _, err := APITokensURL(); err == nil {
				t.Fatalf("APITokensURL() error = nil for %q", dashboardURL)
			}
		})
	}
}
