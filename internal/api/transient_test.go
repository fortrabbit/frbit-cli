package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"
)

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func wrapURL(err error) error {
	return &url.Error{Op: "Get", URL: "https://api.example/v1/x", Err: err}
}

func TestTransientDelayClassifiesErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want bool
	}{
		"429":                {&HTTPError{Status: http.StatusTooManyRequests}, true},
		"503":                {&HTTPError{Status: http.StatusServiceUnavailable}, true},
		"404":                {&HTTPError{Status: http.StatusNotFound}, false},
		"timeout":            {wrapURL(&net.OpError{Op: "read", Net: "tcp", Err: timeoutError{}}), true},
		"connection refused": {wrapURL(&net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}), true},
		"connection reset":   {wrapURL(&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}), true},
		"EOF mid-response":   {wrapURL(io.EOF), true},
		"unexpected EOF":     {wrapURL(io.ErrUnexpectedEOF), true},
		"dns not found":      {wrapURL(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "api.example", IsNotFound: true}}), false},
		"dns timeout":        {wrapURL(&net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "timeout", Name: "api.example", IsTimeout: true}}), true},
		"untrusted cert":     {wrapURL(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}), false},
		"tls alert":          {wrapURL(&net.OpError{Op: "remote error", Err: tls.AlertError(42)}), false},
		"tls record":         {wrapURL(tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}), false},
		"redirect refused":   {wrapURL(errors.New("refusing API redirect to a different origin")), false},
		"cancelled":          {wrapURL(context.Canceled), false},
		"plain error":        {errors.New("decode: bad"), false},
	}
	for name, tc := range cases {
		if _, got := TransientDelay(tc.err, time.Second); got != tc.want {
			t.Errorf("%s: transient = %v, want %v", name, got, tc.want)
		}
	}
}

func TestTransientDelayOnRealRefusedConnection(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	address := server.URL
	server.Close()
	client, err := NewClient(address, "test-token", &http.Client{}, "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetResource(context.Background(), "/v1/apps")
	if _, transient := TransientDelay(err, time.Second); !transient {
		t.Fatalf("refused connection not transient: %v", err)
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", time.Now().Add(5*time.Second).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetResource(context.Background(), "/v1/apps")
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.RetryAfter < 3*time.Second || httpErr.RetryAfter > 6*time.Second {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateCancelDuringRetryWaitReturnsCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "test-token", server.Client(), "frbit/test")
	if err != nil {
		t.Fatal(err)
	}
	client.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	_, err = client.CreateResource(context.Background(), "/v1/apps", map[string]any{"name": "Store"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
