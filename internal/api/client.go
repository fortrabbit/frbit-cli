package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	baseURL, err := url.Parse(strings.TrimRight(host, "/"))
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid API host %q; provide an absolute URL such as https://api.fortrabbit.com", host)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	if httpClient.Timeout == 0 {
		clone := *httpClient
		clone.Timeout = defaultTimeout
		httpClient = &clone
	}

	return &Client{baseURL: baseURL, token: token, http: httpClient, userAgent: userAgent}, nil
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
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: path, RawQuery: query.Encode()})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("User-Agent", c.userAgent)

	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, newHTTPError(response, body)
	}

	return body, nil
}

func newHTTPError(response *http.Response, body []byte) *HTTPError {
	var payload struct {
		Detail           string `json:"detail"`
		Description      string `json:"description"`
		HydraDescription string `json:"hydra:description"`
		Message          string `json:"message"`
	}
	_ = json.Unmarshal(body, &payload)

	message := firstNonEmpty(payload.Detail, payload.Description, payload.HydraDescription, payload.Message)
	retryAfter := time.Duration(0)
	if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
		retryAfter = time.Duration(seconds) * time.Second
	}

	return &HTTPError{Status: response.StatusCode, Message: message, RetryAfter: retryAfter}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
