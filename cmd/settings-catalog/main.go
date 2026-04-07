package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type catalog struct {
	Source   string           `json:"source" yaml:"source"`
	Settings []catalogSetting `json:"settings" yaml:"settings"`
}

type catalogSetting struct {
	Key         string         `json:"key" yaml:"key"`
	ValueType   string         `json:"valueType" yaml:"valueType"`
	Default     string         `json:"default,omitempty" yaml:"default,omitempty"`
	ObjectType  string         `json:"objectType,omitempty" yaml:"objectType,omitempty"`
	ObjectFields []objectField `json:"objectFields,omitempty" yaml:"objectFields,omitempty"`
	Sources     []string       `json:"sources" yaml:"sources"`
}

type objectField struct {
	Name string `json:"name" yaml:"name"`
	Type string `json:"type" yaml:"type"`
}

type structField struct {
	Name string
	Type string
}

type functionSpec struct {
	Name         string
	ValueType    string
	KeyArgStart  int
	Expand       func(prefix []string, defaultValue string, objectType string, objectFields []objectField, source string) []catalogSetting
	ExtractType  func(*ast.CallExpr) (string, []objectField)
}

type scanner struct {
	root        string
	fset        *token.FileSet
	structsByDir map[string]map[string][]objectField
	structsByName map[string][]objectField
}

func main() {
	root := flag.String("root", ".", "Repository root to scan")
	formatName := flag.String("format", "yaml", "Output format: yaml or json")
	output := flag.String("output", "", "Write output to file instead of stdout")
	flag.Parse()

	c, err := newScanner(*root).scan()
	if err != nil {
		fail(err)
	}

	data, err := marshalCatalog(c, *formatName)
	if err != nil {
		fail(err)
	}

	if *output == "" {
		_, _ = os.Stdout.Write(data)
		return
	}

	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	_, _ = fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func marshalCatalog(c catalog, formatName string) ([]byte, error) {
	switch strings.ToLower(formatName) {
	case "yaml", "yml":
		return yaml.Marshal(c)
	case "json":
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported format %q", formatName)
	}
}

func newScanner(root string) *scanner {
	return &scanner{
		root:         root,
		fset:         token.NewFileSet(),
		structsByDir: map[string]map[string][]objectField{},
		structsByName: map[string][]objectField{},
	}
}

func (s *scanner) scan() (catalog, error) {
	files, err := s.goFiles()
	if err != nil {
		return catalog{}, err
	}

	parsedFiles := make(map[string]*ast.File, len(files))
	for _, path := range files {
		file, err := parser.ParseFile(s.fset, path, nil, 0)
		if err != nil {
			return catalog{}, err
		}
		parsedFiles[path] = file
		s.collectStructs(path, file)
	}

	settingsByKey := map[string]*catalogSetting{}
	for path, file := range parsedFiles {
		if err := s.collectSettings(path, file, settingsByKey); err != nil {
			return catalog{}, err
		}
	}

	settings := make([]catalogSetting, 0, len(settingsByKey))
	for _, setting := range settingsByKey {
		slices.Sort(setting.Sources)
		settings = append(settings, *setting)
	}
	slices.SortFunc(settings, func(a, b catalogSetting) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		if a.ValueType < b.ValueType {
			return -1
		}
		if a.ValueType > b.ValueType {
			return 1
		}
		return 0
	})

	return catalog{
		Source:   "code",
		Settings: settings,
	}, nil
}

func (s *scanner) goFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			switch base {
			case ".git", "bin", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"cmd"+string(filepath.Separator)+"settings-catalog"+string(filepath.Separator)) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.Sort(files)
	return files, nil
}

func (s *scanner) collectStructs(path string, file *ast.File) {
	dir := filepath.Dir(path)
	if _, ok := s.structsByDir[dir]; !ok {
		s.structsByDir[dir] = map[string][]objectField{}
	}

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}

			fields := make([]objectField, 0, len(structType.Fields.List))
			for _, field := range structType.Fields.List {
				name := fieldJSONName(field)
				if name == "" {
					continue
				}
				fields = append(fields, objectField{
					Name: name,
					Type: exprString(s.fset, field.Type),
				})
			}
			s.structsByDir[dir][typeSpec.Name.Name] = fields
			if _, exists := s.structsByName[typeSpec.Name.Name]; !exists {
				s.structsByName[typeSpec.Name.Name] = fields
			}
		}
	}
}

func (s *scanner) collectSettings(path string, file *ast.File, settingsByKey map[string]*catalogSetting) error {
	settingsAliases := settingsImportAliases(file)
	dir := filepath.Dir(path)
	if shouldSkipFile(path) {
		return nil
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		funcName, qualifier := calledFunction(call.Fun)
		spec, ok := knownFunction(funcName)
		if !ok {
			return true
		}
		if qualifier != "" && !settingsAliases[qualifier] {
			return true
		}
		if qualifier == "" && file.Name.Name != "settings" {
			return true
		}

		keyParts, defaultValue, ok := extractKeyParts(call, spec.KeyArgStart)
		if !ok {
			return true
		}

		objectType := ""
		var objectFields []objectField
		if spec.ExtractType != nil {
			objectType, objectFields = spec.ExtractType(call)
			if objectType != "" {
				objectFields = s.resolveStructFields(dir, objectType)
			}
		}

		source := relativeSource(s.root, s.fset.Position(call.Pos()))
		for _, setting := range spec.Expand(keyParts, defaultValue, objectType, objectFields, source) {
			if shouldSkipSetting(setting) {
				continue
			}
			mergeSetting(settingsByKey, setting)
		}
		return true
	})

	return nil
}

func settingsImportAliases(file *ast.File) map[string]bool {
	ret := map[string]bool{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		if path != "github.com/formancehq/operator/v3/internal/resources/settings" {
			continue
		}
		if imp.Name != nil {
			ret[imp.Name.Name] = true
		} else {
			ret["settings"] = true
		}
	}
	return ret
}

func knownFunction(name string) (functionSpec, bool) {
	specs := map[string]functionSpec{
		"GetString":          directSpec("string", 2),
		"GetStringOrDefault": directSpec("string", 3),
		"GetStringOrEmpty":   directSpec("string", 2),
		"RequireString":      directSpec("string", 2),
		"GetStringSlice":     directSpec("string[]", 2),
		"GetTrimmedStringSlice": directSpec("string[]", 2),
		"GetURL":             directSpec("uri", 2),
		"RequireURL":         directSpec("uri", 2),
		"GetInt":             directSpec("int", 2),
		"GetInt32":           directSpec("int32", 2),
		"GetInt64":           directSpec("int64", 2),
		"GetIntOrDefault":    directSpec("int", 3),
		"GetInt32OrDefault":  directSpec("int32", 3),
		"GetUInt16OrDefault": directSpec("uint16", 3),
		"GetBool":            directSpec("bool", 2),
		"GetBoolOrDefault":   directSpec("bool", 3),
		"GetBoolOrFalse":     directSpec("bool", 2),
		"GetBoolOrTrue":      directSpec("bool", 2),
		"GetMap":             directSpec("map[string]string", 2),
		"GetMapOrEmpty":      directSpec("map[string]string", 2),
		"GetEnvVars": {
			Name:        "GetEnvVars",
			ValueType:   "map[string]string",
			KeyArgStart: 2,
			Expand: func(prefix []string, _ string, _ string, _ []objectField, source string) []catalogSetting {
				return []catalogSetting{{
					Key:       strings.Join(append(prefix, "env-vars"), "."),
					ValueType: "map[string]string",
					Sources:   []string{source},
				}}
			},
		},
		"GetResourceRequirements": {
			Name:        "GetResourceRequirements",
			KeyArgStart: 2,
			Expand: func(prefix []string, _ string, _ string, _ []objectField, source string) []catalogSetting {
				return []catalogSetting{
					{Key: strings.Join(append(prefix, "limits"), "."), ValueType: "map[string]string", Sources: []string{source}},
					{Key: strings.Join(append(prefix, "requests"), "."), ValueType: "map[string]string", Sources: []string{source}},
					{Key: strings.Join(append(prefix, "claims"), "."), ValueType: "string[]", Sources: []string{source}},
				}
			},
		},
		"GetAs": {
			Name:        "GetAs",
			ValueType:   "object",
			KeyArgStart: 2,
			ExtractType: extractGenericType,
			Expand: func(prefix []string, _ string, objectType string, objectFields []objectField, source string) []catalogSetting {
				return []catalogSetting{{
					Key:          strings.Join(prefix, "."),
					ValueType:    "object",
					ObjectType:   objectType,
					ObjectFields: objectFields,
					Sources:      []string{source},
				}}
			},
		},
	}
	spec, ok := specs[name]
	return spec, ok
}

func directSpec(valueType string, keyArgStart int) functionSpec {
	return functionSpec{
		ValueType:   valueType,
		KeyArgStart: keyArgStart,
		Expand: func(prefix []string, defaultValue string, _ string, _ []objectField, source string) []catalogSetting {
			return []catalogSetting{{
				Key:       strings.Join(prefix, "."),
				ValueType: valueType,
				Default:   defaultValue,
				Sources:   []string{source},
			}}
		},
	}
}

func calledFunction(fun ast.Expr) (string, string) {
	switch typed := fun.(type) {
	case *ast.SelectorExpr:
		if ident, ok := typed.X.(*ast.Ident); ok {
			return typed.Sel.Name, ident.Name
		}
	case *ast.IndexExpr:
		return calledFunction(typed.X)
	case *ast.IndexListExpr:
		return calledFunction(typed.X)
	case *ast.Ident:
		return typed.Name, ""
	}
	return "", ""
}

func extractGenericType(call *ast.CallExpr) (string, []objectField) {
	switch fun := call.Fun.(type) {
	case *ast.IndexExpr:
		switch idx := fun.Index.(type) {
		case *ast.Ident:
			return idx.Name, nil
		case *ast.SelectorExpr:
			return idx.Sel.Name, nil
		}
	case *ast.IndexListExpr:
		if len(fun.Indices) == 1 {
			switch idx := fun.Indices[0].(type) {
			case *ast.Ident:
				return idx.Name, nil
			case *ast.SelectorExpr:
				return idx.Sel.Name, nil
			}
		}
	}
	return "", nil
}

func extractKeyParts(call *ast.CallExpr, keyArgStart int) ([]string, string, bool) {
	if len(call.Args) <= keyArgStart {
		return nil, "", false
	}

	defaultValue := ""
	if keyArgStart == 3 {
		defaultValue = literalOrExpr(call.Args[2])
	}

	var parts []string
	for _, arg := range call.Args[keyArgStart:] {
		part, ok := keyPart(arg)
		if !ok {
			return nil, "", false
		}
		parts = append(parts, part)
	}
	return parts, defaultValue, true
}

func keyPart(expr ast.Expr) (string, bool) {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return typed.Value, true
		}
		value, err := strconv.Unquote(typed.Value)
		if err != nil {
			return "", false
		}
		return value, true
	case *ast.Ident:
		return placeholderForName(typed.Name), true
	case *ast.SelectorExpr:
		return placeholderForSelector(typed), true
	case *ast.CallExpr:
		return placeholderForCall(typed), true
	default:
		return "", false
	}
}

func placeholderForName(name string) string {
	switch name {
	case "moduleName":
		return "<module-name>"
	case "serviceName":
		return "<service-name>"
	case "monitoringType":
		return "<monitoring-type>"
	case "dnsType":
		return "<dns-type>"
	case "registry":
		return "<name>"
	case "imageWithoutRegistry":
		return "<path>"
	case "kind":
		return "<owner-kind>"
	default:
		if name == "module" || strings.Contains(strings.ToLower(name), "module") {
			return "<module-name>"
		}
		if strings.Contains(strings.ToLower(name), "deployment") {
			return "<deployment-name>"
		}
		if strings.Contains(strings.ToLower(name), "container") {
			return "<container-name>"
		}
		if strings.Contains(strings.ToLower(name), "service") {
			return "<service-name>"
		}
		if strings.Contains(strings.ToLower(name), "registry") {
			return "<name>"
		}
		return "<" + kebab(name) + ">"
	}
}

func placeholderForSelector(expr *ast.SelectorExpr) string {
	left := strings.ToLower(exprString(token.NewFileSet(), expr.X))
	if expr.Sel.Name == "Name" {
		switch {
		case strings.Contains(left, "deployment"):
			return "<deployment-name>"
		case strings.Contains(left, "container"):
			return "<container-name>"
		}
	}
	if expr.Sel.Name == "Kind" {
		return "<owner-kind>"
	}
	if expr.Sel.Name == "Service" {
		return "<module-name>"
	}
	return placeholderForName(expr.Sel.Name)
}

func placeholderForCall(expr *ast.CallExpr) string {
	rendered := exprString(token.NewFileSet(), expr)
	switch {
	case strings.Contains(rendered, "GetKind("):
		return "<owner-kind>"
	case strings.Contains(rendered, "ToLower") && strings.Contains(rendered, "monitoringType"):
		return "<monitoring-type>"
	case strings.Contains(rendered, "LowerCamelCaseKind"):
		return "<owner-kind>"
	default:
		return "<dynamic>"
	}
}

func literalOrExpr(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.BasicLit:
		if typed.Kind == token.STRING {
			value, err := strconv.Unquote(typed.Value)
			if err == nil {
				return value
			}
		}
		return typed.Value
	default:
		return exprString(token.NewFileSet(), expr)
	}
}

func mergeSetting(dst map[string]*catalogSetting, setting catalogSetting) {
	key := setting.Key + "|" + setting.ValueType
	existing, ok := dst[key]
	if !ok {
		copySetting := setting
		dst[key] = &copySetting
		return
	}
	if existing.Default == "" {
		existing.Default = setting.Default
	}
	if existing.ObjectType == "" {
		existing.ObjectType = setting.ObjectType
	}
	if len(existing.ObjectFields) == 0 && len(setting.ObjectFields) > 0 {
		existing.ObjectFields = slices.Clone(setting.ObjectFields)
	}
	existing.Sources = append(existing.Sources, setting.Sources...)
	existing.Sources = uniqueStrings(existing.Sources)
}

func (s *scanner) resolveStructFields(dir, typeName string) []objectField {
	if fields := s.structsByDir[dir][typeName]; len(fields) > 0 {
		return slices.Clone(fields)
	}
	if fields := s.structsByName[typeName]; len(fields) > 0 {
		return slices.Clone(fields)
	}
	return nil
}

func shouldSkipFile(path string) bool {
	slashed := filepath.ToSlash(path)
	return strings.HasSuffix(slashed, "/internal/resources/settings/helpers.go") ||
		strings.HasSuffix(slashed, "/internal/resources/settings/resourcerequirements.go")
}

func shouldSkipSetting(setting catalogSetting) bool {
	return strings.Contains(setting.Key, "<dynamic>") || strings.Contains(setting.Key, "<keys>")
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	ret := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		ret = append(ret, value)
	}
	return ret
}

func relativeSource(root string, pos token.Position) string {
	rel, err := filepath.Rel(root, pos.Filename)
	if err != nil {
		rel = pos.Filename
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line)
}

func fieldJSONName(field *ast.Field) string {
	if field.Tag != nil {
		tagValue, err := strconv.Unquote(field.Tag.Value)
		if err == nil {
			tag := reflectJSONTag(tagValue)
			if tag != "" && tag != "-" {
				return tag
			}
		}
	}
	if len(field.Names) == 1 {
		return kebab(field.Names[0].Name)
	}
	return ""
}

func reflectJSONTag(tag string) string {
	for _, part := range strings.Split(tag, " ") {
		if !strings.HasPrefix(part, `json:"`) {
			continue
		}
		value := strings.TrimPrefix(part, `json:"`)
		value = strings.TrimSuffix(value, `"`)
		value = strings.Split(value, ",")[0]
		return value
	}
	return ""
}

func exprString(fset *token.FileSet, expr ast.Expr) string {
	var builder strings.Builder
	if err := format.Node(&builder, fset, expr); err != nil {
		return ""
	}
	return builder.String()
}

func kebab(value string) string {
	var parts []rune
	for i, r := range value {
		if i > 0 && r >= 'A' && r <= 'Z' {
			parts = append(parts, '-')
		}
		parts = append(parts, rune(strings.ToLower(string(r))[0]))
	}
	return string(parts)
}
