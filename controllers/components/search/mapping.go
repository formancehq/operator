package search

// TODO: Code copied from search project, import it when the project will be public

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/opensearch-project/opensearch-go"
	"github.com/opensearch-project/opensearch-go/opensearchapi"
	"github.com/pkg/errors"
)

type Property struct {
	Mappings
	Type    string `json:"type,omitempty"`
	Store   bool   `json:"store,omitempty"`
	CopyTo  string `json:"copy_to,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type DynamicTemplate map[string]interface{}

type Mappings struct {
	DynamicTemplates []DynamicTemplate   `json:"dynamic_templates,omitempty"`
	Properties       map[string]Property `json:"properties,omitempty"`
}

type Template struct {
	IndexPatterns []string `json:"index_patterns"`
	Mappings      Mappings `json:"mappings"`
}

func DefaultMapping(patterns ...string) Template {
	f := false
	return Template{
		IndexPatterns: patterns,
		Mappings: Mappings{
			DynamicTemplates: []DynamicTemplate{
				{
					"strings": map[string]interface{}{
						"match_mapping_type": "string",
						"mapping": map[string]interface{}{
							"type": "keyword",
						},
					},
				},
			},
			Properties: map[string]Property{
				"kind": {
					Type: "keyword",
				},
				"ledger": {
					Type: "keyword",
				},
				"when": {
					Type: "date",
				},
				"data": {
					Type:    "object",
					Enabled: &f,
				},
				"indexed": {
					Type: "object",
				},
			},
		},
	}
}

func LoadMapping(ctx context.Context, client *opensearch.Client, m Template, searchName string) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}

	res, err := opensearchapi.IndicesPutTemplateRequest{
		Body: bytes.NewReader(data),
		Name: fmt.Sprintf("%s_search_mapping", searchName),
	}.Do(ctx, client)

	if err != nil {
		return err
	}
	if res.IsError() {
		return errors.New(res.String())
	}
	return nil
}
