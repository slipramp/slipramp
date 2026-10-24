// Package argodiff defines the structured Argo CD diff artifact format.
package argodiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const MediaType = "application/vnd.slipramp.argocd-diff+json"

type Document struct {
	Resources []Resource `json:"resources"`
}

type Resource struct {
	Group               string `json:"group,omitempty"`
	Kind                string `json:"kind"`
	Namespace           string `json:"namespace,omitempty"`
	Name                string `json:"name"`
	Diff                string `json:"diff"`
	VersionMetadataOnly bool   `json:"versionMetadataOnly,omitempty"`
}

func Decode(contents []byte) (Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()

	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("unexpected data after JSON object")
		}

		return Document{}, err
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}

	return document, nil
}

func (d Document) Validate() error {
	if d.Resources == nil {
		return errors.New("resources must be an array")
	}
	for index, resource := range d.Resources {
		if strings.TrimSpace(resource.Kind) == "" || strings.TrimSpace(resource.Name) == "" ||
			strings.TrimSpace(resource.Diff) == "" {
			return fmt.Errorf("resource %d requires kind, name, and diff", index+1)
		}
	}

	return nil
}
