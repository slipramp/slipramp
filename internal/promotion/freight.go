package promotion

import "slices"

type Freight struct {
	Items []FreightItem `json:"items"`
}

type FreightRequest struct {
	Origin       string   `json:"origin"`
	Direct       bool     `json:"direct"`
	SourceStages []string `json:"sourceStages"`
}

type FreightItem struct {
	Origin    string            `json:"origin"`
	Name      string            `json:"name"`
	Artifacts []FreightArtifact `json:"artifacts"`
}

type FreightArtifact struct {
	Kind       string  `json:"kind"`
	Repository *string `json:"repository,omitempty"`
	Version    string  `json:"version"`
}

func cloneFreight(freight Freight) Freight {
	items := make([]FreightItem, len(freight.Items))
	for index, item := range freight.Items {
		items[index] = cloneFreightItem(item)
	}

	return Freight{Items: items}
}

func cloneFreightItem(item FreightItem) FreightItem {
	artifacts := make([]FreightArtifact, len(item.Artifacts))
	for index, value := range item.Artifacts {
		artifacts[index] = value
		if value.Repository != nil {
			repository := *value.Repository
			artifacts[index].Repository = &repository
		}
	}
	item.Artifacts = artifacts

	return item
}

func cloneFreightRequests(requests []FreightRequest) []FreightRequest {
	cloned := slices.Clone(requests)
	for index := range cloned {
		cloned[index].SourceStages = slices.Clone(cloned[index].SourceStages)
	}

	return cloned
}
