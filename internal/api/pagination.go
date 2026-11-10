package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxPages stops a server whose next links never end.
const maxPages = 1000

// ListAll follows the Link rel="next" pages from path and returns up to limit resources, or all when limit is 0.
// Raw is a JSON array of the returned resources; TotalItems comes from X-Total-Count.
func (c Client) ListAll(ctx context.Context, path string, query url.Values, limit int) (ResourcesResponse, error) {
	resources := []Resource{}
	totalItems := -1
	for page := 0; ; page++ {
		if page >= maxPages {
			return ResourcesResponse{}, fmt.Errorf("stopped after %d pages", maxPages)
		}
		response, err := c.do(ctx, call{method: http.MethodGet, path: path, query: query, accept: "application/json"})
		if err != nil {
			return ResourcesResponse{}, err
		}
		pageResources, _, err := decodeResources(response.body)
		if err != nil {
			return ResourcesResponse{}, err
		}
		if totalItems < 0 {
			if total, err := strconv.Atoi(strings.TrimSpace(response.header.Get("X-Total-Count"))); err == nil && total >= 0 {
				totalItems = total
			}
		}
		resources = append(resources, pageResources...)
		if limit > 0 && len(resources) >= limit {
			resources = resources[:limit]
			break
		}
		next := nextLink(strings.Join(response.header.Values("Link"), ","))
		if next == "" || len(pageResources) == 0 {
			break
		}
		path, query, err = c.sameOriginPath(next)
		if err != nil {
			return ResourcesResponse{}, err
		}
	}
	if totalItems < 0 {
		totalItems = len(resources)
	}
	raw, err := json.Marshal(resources)
	if err != nil {
		return ResourcesResponse{}, fmt.Errorf("encode resource collection: %w", err)
	}
	return ResourcesResponse{Resources: resources, TotalItems: totalItems, Raw: raw}, nil
}

// sameOriginPath keeps the bearer token on the API origin when following a next link.
func (c Client) sameOriginPath(link string) (string, url.Values, error) {
	reference, err := url.Parse(link)
	if err != nil {
		return "", nil, fmt.Errorf("invalid next page link: %w", err)
	}
	target := c.baseURL.ResolveReference(reference)
	if !sameOrigin(c.baseURL, target) {
		return "", nil, fmt.Errorf("refusing next page link to a different origin")
	}
	return target.Path, target.Query(), nil
}

// nextLink returns the target of the rel="next" entry of an RFC 8288 Link header.
func nextLink(header string) string {
	rest := header
	for {
		start := strings.IndexByte(rest, '<')
		if start < 0 {
			return ""
		}
		end := strings.IndexByte(rest[start:], '>')
		if end < 0 {
			return ""
		}
		target := rest[start+1 : start+end]
		rest = rest[start+end+1:]
		params := rest
		if following := strings.IndexByte(rest, '<'); following >= 0 {
			params = rest[:following]
		}
		for _, param := range strings.Split(params, ";") {
			name, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(name), "rel") {
				continue
			}
			value = strings.Trim(strings.TrimRight(strings.TrimSpace(value), ", "), `"`)
			for _, rel := range strings.Fields(value) {
				if strings.EqualFold(rel, "next") {
					return target
				}
			}
		}
	}
}
