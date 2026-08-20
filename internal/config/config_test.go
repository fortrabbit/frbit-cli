package config

import "testing"

func TestTokenCreationURL(t *testing.T) {
	tests := []struct {
		name      string
		dashboard string
		want      string
	}{
		{name: "production default", want: "https://dash.fortrabbit.com/new/api-token"},
		{name: "development override", dashboard: "http://localhost:3001", want: "http://localhost:3001/new/api-token"},
		{name: "trailing slash", dashboard: "http://localhost:3001/", want: "http://localhost:3001/new/api-token"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("FRBIT_DASHBOARD_URL", test.dashboard)
			got, err := TokenCreationURL()
			if err != nil {
				t.Fatalf("TokenCreationURL() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("TokenCreationURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestTokenCreationURLRejectsInvalidOverride(t *testing.T) {
	tests := []string{
		"localhost:3001",
		"file:///tmp/token",
		"https://dashboard.example.com/path",
		"https://dashboard.example.com?next=/new/api-token",
	}

	for _, dashboardURL := range tests {
		t.Run(dashboardURL, func(t *testing.T) {
			t.Setenv("FRBIT_DASHBOARD_URL", dashboardURL)
			if _, err := TokenCreationURL(); err == nil {
				t.Fatalf("TokenCreationURL() error = nil for %q", dashboardURL)
			}
		})
	}
}
