package config

import "testing"

func TestDashboardURL(t *testing.T) {
	tests := []struct {
		name      string
		dashboard string
		want      string
	}{
		{name: "production default", want: "https://dash.fortrabbit.com"},
		{name: "development override", dashboard: "http://localhost:3001", want: "http://localhost:3001"},
		{name: "trailing slash", dashboard: "http://localhost:3001/", want: "http://localhost:3001"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("FRBIT_DASHBOARD_URL", test.dashboard)
			got, err := DashboardURL()
			if err != nil {
				t.Fatalf("DashboardURL() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("DashboardURL() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDashboardURLRejectsInvalidOverride(t *testing.T) {
	tests := []string{
		"localhost:3001",
		"file:///tmp/token",
		"https://dashboard.example.com/path",
		"https://dashboard.example.com?next=/new/api-token",
	}

	for _, dashboardURL := range tests {
		t.Run(dashboardURL, func(t *testing.T) {
			t.Setenv("FRBIT_DASHBOARD_URL", dashboardURL)
			if _, err := DashboardURL(); err == nil {
				t.Fatalf("DashboardURL() error = nil for %q", dashboardURL)
			}
		})
	}
}
