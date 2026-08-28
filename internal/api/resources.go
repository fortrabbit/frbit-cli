package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// Resource is an API resource represented as its JSON object. The public API
// resources do not share a stable shape, so commands retain the original
// fields instead of discarding information during decoding.
type Resource map[string]any

type ResourcesResponse struct {
	Resources  []Resource
	TotalItems int
	Raw        []byte
}

type ResourceResponse struct {
	Resource Resource
	Raw      []byte
}

func (c Client) CreateResource(ctx context.Context, path string, payload any) (ResourceResponse, error) {
	body, err := c.post(ctx, path, payload)
	if err != nil {
		return ResourceResponse{}, err
	}
	return decodeResourceResponse(body)
}

func (c Client) UpdateResource(ctx context.Context, path string, payload any) (ResourceResponse, error) {
	body, err := c.patch(ctx, path, payload)
	if err != nil {
		return ResourceResponse{}, err
	}
	return decodeResourceResponse(body)
}

func (c Client) DeleteResource(ctx context.Context, path string) error {
	_, err := c.delete(ctx, path)
	return err
}

// PostAction invokes an action endpoint that may return either a resource or
// an empty successful response.
func (c Client) PostAction(ctx context.Context, path string) (ResourceResponse, error) {
	body, err := c.post(ctx, path, nil)
	if err != nil {
		return ResourceResponse{}, err
	}
	if len(body) == 0 {
		return ResourceResponse{Raw: body}, nil
	}
	return decodeResourceResponse(body)
}

func (c Client) ListResources(ctx context.Context, path string, page int, publicIDs []string) (ResourcesResponse, error) {
	return c.listResources(ctx, path, page, publicIDs, false)
}

// ListResourcesWithTotal requests collection metadata so TotalItems covers all
// matching pages, rather than only the resources in the current response.
func (c Client) ListResourcesWithTotal(ctx context.Context, path string, page int, publicIDs []string) (ResourcesResponse, error) {
	return c.listResources(ctx, path, page, publicIDs, true)
}

func (c Client) listResources(ctx context.Context, path string, page int, publicIDs []string, includeTotal bool) (ResourcesResponse, error) {
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	for _, publicID := range publicIDs {
		query.Add("publicId[]", publicID)
	}

	var body []byte
	var err error
	if includeTotal {
		body, err = c.getCollection(ctx, path, query)
	} else {
		body, err = c.get(ctx, path, query)
	}
	if err != nil {
		return ResourcesResponse{}, err
	}
	resources, totalItems, err := decodeResources(body)
	if err != nil {
		return ResourcesResponse{}, err
	}
	return ResourcesResponse{Resources: resources, TotalItems: totalItems, Raw: body}, nil
}

func (c Client) GetResource(ctx context.Context, path string) (ResourceResponse, error) {
	body, err := c.get(ctx, path, nil)
	if err != nil {
		return ResourceResponse{}, err
	}
	return decodeResourceResponse(body)
}

func decodeResourceResponse(body []byte) (ResourceResponse, error) {
	var resource Resource
	if err := json.Unmarshal(body, &resource); err != nil {
		return ResourceResponse{}, fmt.Errorf("decode resource response: %w", err)
	}
	return ResourceResponse{Resource: resource, Raw: body}, nil
}

func decodeResources(body []byte) ([]Resource, int, error) {
	var resources []Resource
	if err := json.Unmarshal(body, &resources); err == nil {
		return resources, len(resources), nil
	}

	var paginated struct {
		Member          []Resource `json:"member"`
		HydraMember     []Resource `json:"hydra:member"`
		TotalItems      *int       `json:"totalItems"`
		HydraTotalItems *int       `json:"hydra:totalItems"`
	}
	if err := json.Unmarshal(body, &paginated); err != nil {
		return nil, 0, fmt.Errorf("decode resource collection: %w", err)
	}
	totalItems := paginated.TotalItems
	if totalItems == nil {
		totalItems = paginated.HydraTotalItems
	}
	if paginated.Member != nil {
		return paginated.Member, collectionTotal(paginated.Member, totalItems), nil
	}
	if paginated.HydraMember != nil {
		return paginated.HydraMember, collectionTotal(paginated.HydraMember, totalItems), nil
	}

	return nil, 0, fmt.Errorf("decode resource collection: expected an array or a paginated member collection")
}

func collectionTotal(resources []Resource, totalItems *int) int {
	if totalItems != nil {
		return *totalItems
	}
	return len(resources)
}
