package api

import (
	"encoding/json"
	"fmt"
)

type App struct {
	PublicID    string  `json:"publicId"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Trial       bool    `json:"trial"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
}

type AppsResponse struct {
	Apps []App
	Raw  []byte
}

func decodeApps(body []byte) (AppsResponse, error) {
	var apps []App
	if err := json.Unmarshal(body, &apps); err == nil {
		return AppsResponse{Apps: apps}, nil
	}

	var paginated struct {
		Member      []App `json:"member"`
		HydraMember []App `json:"hydra:member"`
	}
	if err := json.Unmarshal(body, &paginated); err != nil {
		return AppsResponse{}, fmt.Errorf("decode apps response: %w", err)
	}
	if paginated.Member != nil {
		return AppsResponse{Apps: paginated.Member}, nil
	}
	if paginated.HydraMember != nil {
		return AppsResponse{Apps: paginated.HydraMember}, nil
	}

	return AppsResponse{}, fmt.Errorf("decode apps response: expected an array or a paginated member collection")
}
