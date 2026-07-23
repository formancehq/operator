{{- define "type" -}}
{{- $type := .Type -}}
{{- $recurse := .Recurse -}}
{{- $prefix := .Prefix -}}
{{- $ctx := . -}}

{{ repeat ($prefix | int) "#" }} {{ $type.Name }}

{{ if $type.IsAlias }}_Underlying type:_ _{{ markdownRenderTypeLink $type.UnderlyingType  }}_{{ end }}

{{ $type.Doc }}

{{/*{{ if $type.Validation -}}*/}}
{{/*_Validation:_*/}}
{{/*{{- range $type.Validation }}*/}}
{{/*- {{ . }}*/}}
{{/*{{- end }}*/}}
{{/*{{- end }}*/}}

{{/*{{ if $type.References -}}*/}}
{{/*_Appears in:_*/}}
{{/*{{- range $type.SortedReferences }}*/}}
{{/*- {{ markdownRenderTypeLink . }}*/}}
{{/*{{- end }}*/}}
{{/*{{- end }}*/}}

{{ if $type.Members -}}
| Field | Description | Default | Validation |
| --- | --- | --- | --- |
{{ if $type.GVK -}}
| `apiVersion` _string_ | `{{ $type.GVK.Group }}/{{ $type.GVK.Version }}` | | |
| `kind` _string_ | `{{ $type.GVK.Kind }}` | | |
{{ end -}}

{{ range $type.Members -}}
{{- if or (has "Type: string" .Type.Validation) (eq (markdownRenderType .Type) "[Duration](#duration)") -}}
| `{{ .Name  }}` _string_ | {{ template "type_members" . }} | {{ markdownRenderDefault .Default }} | {{ range .Validation -}} {{ . }} <br />{{ end }} |
{{- else -}}
| `{{ .Name  }}` _{{ markdownRenderType .Type }}_ | {{ template "type_members" . }} | {{ markdownRenderDefault .Default }} | {{ range .Validation -}} {{ . }} <br />{{ end }} |
{{- end }}
{{ end -}}

{{ end -}}

{{- if $recurse }}
{{- if eq $type.Kind 4}}
{{- $dummy := set $ctx "Fields" $type.UnderlyingType.Fields }}
{{- else }}
{{- $dummy := set $ctx "Fields" $type.Fields }}
{{- end }}
{{- range $k, $field := $ctx.Fields }}
{{- if hasPrefix "github.com/formancehq/operator/v3/api" $field.Type.Package }}
{{- if has "Type: string" $field.Type.Validation }}{{ continue }}{{ end }}
{{- $seenKey := printf "_seen_%s" $field.Type.Name }}
{{- if hasKey $ctx $seenKey }}{{ continue }}{{ end }}
{{- $_ := set $ctx $seenKey true }}
{{ template "type" (dict "Type" $field.Type "Recurse" true "Prefix" (min (add $prefix 1) 6)) }}
{{- end }}
{{- end }}
{{- end }}

{{- end -}}
