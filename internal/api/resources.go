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
	Resources []Resource
	Raw       []byte
}

type ResourceResponse struct {
	Resource Resource
	Raw      []byte
}

func (c Client) ListResources(ctx context.Context, path string, page int, publicIDs []string) (ResourcesResponse, error) {
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	for _, publicID := range publicIDs {
		query.Add("publicId[]", publicID)
	}

	body, err := c.get(ctx, path, query)
	if err != nil {
		return ResourcesResponse{}, err
	}
	resources, err := decodeResources(body)
	if err != nil {
		return ResourcesResponse{}, err
	}
	return ResourcesResponse{Resources: resources, Raw: body}, nil
}

func (c Client) GetResource(ctx context.Context, path string) (ResourceResponse, error) {
	body, err := c.get(ctx, path, nil)
	if err != nil {
		return ResourceResponse{}, err
	}

	var resource Resource
	if err := json.Unmarshal(body, &resource); err != nil {
		return ResourceResponse{}, fmt.Errorf("decode resource response: %w", err)
	}
	return ResourceResponse{Resource: resource, Raw: body}, nil
}

func decodeResources(body []byte) ([]Resource, error) {
	var resources []Resource
	if err := json.Unmarshal(body, &resources); err == nil {
		return resources, nil
	}

	var paginated struct {
		Member      []Resource `json:"member"`
		HydraMember []Resource `json:"hydra:member"`
	}
	if err := json.Unmarshal(body, &paginated); err != nil {
		return nil, fmt.Errorf("decode resource collection: %w", err)
	}
	if paginated.Member != nil {
		return paginated.Member, nil
	}
	if paginated.HydraMember != nil {
		return paginated.HydraMember, nil
	}

	return nil, fmt.Errorf("decode resource collection: expected an array or a paginated member collection")
}
