package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

type Client struct {
	baseURL   *url.URL
	token     string
	http      *http.Client
	userAgent string
}

type HTTPError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *HTTPError) Error() string {
	message := fmt.Sprintf("public API request failed with HTTP %d", e.Status)
	if e.Message != "" {
		message += ": " + e.Message
	}
	if e.RetryAfter > 0 {
		message += fmt.Sprintf("; retry after %s", e.RetryAfter)
	}
	return message
}

func NewClient(host string, token string, httpClient *http.Client, userAgent string) (*Client, error) {
	baseURL, err := parseBaseURL(host)
	if err != nil {
		return nil, err
	}
	httpClient = clientForOrigin(httpClient, baseURL)

	return &Client{baseURL: baseURL, token: token, http: httpClient, userAgent: userAgent}, nil
}

func parseBaseURL(host string) (*url.URL, error) {
	baseURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(host), "/"))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid API host; provide an absolute URL such as https://api.fortrabbit.com")
	}
	if baseURL.User != nil {
		return nil, fmt.Errorf("API host must not contain credentials")
	}
	if (baseURL.Path != "" && baseURL.Path != "/") || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("API host must be an origin without a path, query, or fragment")
	}

	baseURL.Path = ""
	baseURL.Scheme = strings.ToLower(baseURL.Scheme)
	switch baseURL.Scheme {
	case "https":
		return baseURL, nil
	case "http":
		if isLoopbackHost(baseURL.Hostname()) {
			return baseURL, nil
		}
		return nil, fmt.Errorf("API host must use HTTPS; HTTP is allowed only for loopback addresses")
	default:
		return nil, fmt.Errorf("API host must use HTTPS; HTTP is allowed only for loopback addresses")
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func clientForOrigin(httpClient *http.Client, origin *url.URL) *http.Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	clone := *httpClient
	if clone.Timeout == 0 {
		clone.Timeout = defaultTimeout
	}
	previousCheckRedirect := clone.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !sameOrigin(origin, request.URL) {
			return fmt.Errorf("refusing API redirect to a different origin")
		}
		if previousCheckRedirect != nil {
			return previousCheckRedirect(request, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	return &clone
}

func sameOrigin(first *url.URL, second *url.URL) bool {
	return strings.EqualFold(first.Scheme, second.Scheme) &&
		strings.EqualFold(first.Hostname(), second.Hostname()) &&
		effectivePort(first) == effectivePort(second)
}

func effectivePort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (c Client) Apps(ctx context.Context, page int) (AppsResponse, error) {
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	return c.getApps(ctx, "/v1/apps", query)
}

func (c Client) CheckToken(ctx context.Context) error {
	_, err := c.Apps(ctx, 1)
	return err
}

func (c Client) getApps(ctx context.Context, path string, query url.Values) (AppsResponse, error) {
	body, err := c.get(ctx, path, query)
	if err != nil {
		return AppsResponse{}, err
	}

	response, err := decodeApps(body)
	if err != nil {
		return AppsResponse{}, err
	}
	response.Raw = body
	return response, nil
}

func (c Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.request(ctx, http.MethodGet, path, query, nil, "")
}

func (c Client) getCollection(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.requestWithAccept(ctx, http.MethodGet, path, query, nil, "", "application/ld+json")
}

func (c Client) post(ctx context.Context, path string, payload any) ([]byte, error) {
	body, err := marshalPayload(payload)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodPost, path, nil, body, "application/json")
}

func (c Client) patch(ctx context.Context, path string, payload any) ([]byte, error) {
	body, err := marshalPayload(payload)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodPatch, path, nil, body, "application/merge-patch+json")
}

func (c Client) delete(ctx context.Context, path string) ([]byte, error) {
	return c.request(ctx, http.MethodDelete, path, nil, nil, "")
}

func marshalPayload(payload any) ([]byte, error) {
	if payload == nil {
		return nil, nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request payload: %w", err)
	}
	return body, nil
}

func (c Client) request(ctx context.Context, method string, path string, query url.Values, body []byte, contentType string) ([]byte, error) {
	return c.requestWithAccept(ctx, method, path, query, body, contentType, "application/json")
}

func (c Client) requestWithAccept(ctx context.Context, method string, path string, query url.Values, body []byte, contentType string, accept string) ([]byte, error) {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()})
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("User-Agent", c.userAgent)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, newHTTPError(response, responseBody)
	}

	return responseBody, nil
}

func newHTTPError(response *http.Response, body []byte) *HTTPError {
	var payload struct {
		Error            string         `json:"error"`
		Detail           string         `json:"detail"`
		Description      string         `json:"description"`
		HydraDescription string         `json:"hydra:description"`
		Message          string         `json:"message"`
		Errors           map[string]any `json:"errors"`
		Violations       []struct {
			PropertyPath string `json:"propertyPath"`
			Message      string `json:"message"`
		} `json:"violations"`
	}
	_ = json.Unmarshal(body, &payload)

	message := firstNonEmpty(payload.Error, payload.Detail, payload.Description, payload.HydraDescription, payload.Message)
	if message == "" {
		message = validationMessage(payload.Errors, payload.Violations)
	}
	retryAfter := time.Duration(0)
	if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
		retryAfter = time.Duration(seconds) * time.Second
	}

	return &HTTPError{Status: response.StatusCode, Message: message, RetryAfter: retryAfter}
}

func validationMessage(errors map[string]any, violations []struct {
	PropertyPath string `json:"propertyPath"`
	Message      string `json:"message"`
}) string {
	parts := make([]string, 0, len(errors)+len(violations))
	keys := make([]string, 0, len(errors))
	for key := range errors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := errors[key]
		if text, ok := value.(string); ok {
			parts = append(parts, key+": "+text)
			continue
		}
		encoded, err := json.Marshal(value)
		if err == nil {
			parts = append(parts, key+": "+string(encoded))
		}
	}
	for _, violation := range violations {
		if violation.PropertyPath != "" {
			parts = append(parts, violation.PropertyPath+": "+violation.Message)
		} else if violation.Message != "" {
			parts = append(parts, violation.Message)
		}
	}
	return strings.Join(parts, "; ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
