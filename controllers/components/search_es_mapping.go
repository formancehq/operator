package components

// TODO: Code copied from search project, import it when the project will be public

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

func GetMapping() Mappings {
	f := false
	return Mappings{
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
	}
}