package app

import (
	"testing"
	"time"
)

func TestNewFactorySetsHTTPTimeout(t *testing.T) {
	if got := NewFactory("test", "test", "test").HTTPClient.Timeout; got != 30*time.Second {
		t.Fatalf("HTTP timeout = %s, want %s", got, 30*time.Second)
	}
}
