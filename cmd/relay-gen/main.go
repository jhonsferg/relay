// Package main implements relay-gen, an OpenAPI 3.x client code generator
// that produces type-safe Go clients using the relay library.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"unicode"
)

// ── OpenAPI 3.x schema types ────────────────────────────────────────────────

type openAPISpec struct {
	Paths      map[string]pathItem `json:"paths"`
	Components components          `json:"components"`
}

type components struct {
	Schemas map[string]schemaObject `json:"schemas"`
}

type pathItem struct {
	Get    *operation `json:"get"`
	Post   *operation `json:"post"`
	Put    *operation `json:"put"`
	Patch  *operation `json:"patch"`
	Delete *operation `json:"delete"`
}

type operation struct {
	OperationID string       `json:"operationId"`
	Parameters  []parameter  `json:"parameters"`
	RequestBody *requestBody `json:"requestBody"`
}

type parameter struct {
	Name     string       `json:"name"`
	In       string       `json:"in"` // "query", "path", "header", "cookie"
	Required bool         `json:"required"`
	Schema   schemaObject `json:"schema"`
}

type requestBody struct {
	Required bool                       `json:"required"`
	Content  map[string]mediaTypeObject `json:"content"`
}

type mediaTypeObject struct {
	Schema schemaObject `json:"schema"`
}

type schemaObject struct {
	Type       string                  `json:"type"`
	Format     string                  `json:"format"`
	Ref        string                  `json:"$ref"`
	Properties map[string]schemaObject `json:"properties"`
	Required   []string                `json:"required"`
}

// ── Code generation data types ───────────────────────────────────────────────

type clientData struct {
	Package string
	Module  string
	Methods []methodData
	HasFmt  bool
}

type methodData struct {
	FuncName   string
	HTTPMethod string
	Path       string
	Params     []paramData
	HasBody    bool
	HasQuery   bool
	HasPath    bool
}

type paramData struct {
	GoName   string
	APIName  string
	Kind     string // "query", "path"
	GoType   string
	Optional bool
}

type modelsData struct {
	Package string
	Schemas []schemaData
}

type schemaData struct {
	Name   string
	Fields []fieldData
}

type fieldData struct {
	Name    string
	JSONTag string
	GoType  string
}

// ── Templates ────────────────────────────────────────────────────────────────

const clientTemplate = `package {{.Package}}

import (
	"context"
{{- if .HasFmt}}
	"fmt"
{{- end}}

	"{{.Module}}"
)

// Client wraps a relay.Client for the API.
type Client struct {
	inner *relay.Client
}

// New creates a Client. Pass relay options to configure base URL, auth, etc.
func New(opts ...relay.Option) *Client {
	return &Client{inner: relay.New(opts...)}
}
{{range .Methods}}
// {{.FuncName}} calls {{.HTTPMethod}} {{.Path}}
func (c *Client) {{.FuncName}}(ctx context.Context{{range .Params}}, {{.GoName}} {{.GoType}}{{end}}{{if .HasBody}}, body interface{}{{end}}) (*relay.Response, error) {
	req := c.inner.{{httpMethodFunc .HTTPMethod}}({{pathLiteral .Path .Params}})
{{- range .Params}}{{if eq .Kind "path"}}
	req = req.WithPathParam("{{.APIName}}", {{.GoName}})
{{- end}}{{end}}
{{- range .Params}}{{if eq .Kind "query"}}
{{- if .Optional}}
	if {{.GoName}} != nil {
		req = req.WithQueryParam("{{.APIName}}", {{formatQueryParam .GoType .GoName}})
	}
{{- else}}
	req = req.WithQueryParam("{{.APIName}}", {{formatQueryParam .GoType .GoName}})
{{- end}}
{{- end}}{{end}}
{{- if .HasBody}}
	req = req.WithJSON(body)
{{- end}}
	return c.inner.Execute(req.WithContext(ctx))
}
{{end}}`

const modelsTemplate = `package {{.Package}}
{{if .Schemas}}
// Schema models generated from components/schemas.
{{range .Schemas}}
// {{.Name}} is a generated model.
type {{.Name}} struct {
{{- range .Fields}}
	{{.Name}} {{.GoType}} ` + "`" + `json:"{{.JSONTag}}"` + "`" + `
{{- end}}
}
{{end}}{{end}}`

// ── Helper functions ─────────────────────────────────────────────────────────

// pascalCase converts identifiers like "listUsers" or "get_user" to "ListUsers" / "GetUser".
func pascalCase(s string) string {
	if s == "" {
		return s
	}
	// Split on underscore, hyphen, and camelCase boundaries.
	var words []string
	var cur strings.Builder
	for i, r := range s {
		if r == '_' || r == '-' {
			if cur.Len() > 0 {
				words = append(words, cur.String())
				cur.Reset()
			}
			continue
		}
		// Detect camelCase upper-case boundary (not at index 0).
		if i > 0 && unicode.IsUpper(r) && cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		words = append(words, cur.String())
	}
	var b strings.Builder
	for _, w := range words {
		if len(w) == 0 {
			continue
		}
		runes := []rune(w)
		b.WriteRune(unicode.ToUpper(runes[0]))
		b.WriteString(string(runes[1:]))
	}
	return b.String()
}

// schemaGoType maps an OpenAPI schema type/format to a Go type.
func schemaGoType(s schemaObject) string {
	if s.Ref != "" {
		parts := strings.Split(s.Ref, "/")
		return "*" + pascalCase(parts[len(parts)-1])
	}
	switch s.Type {
	case "integer":
		if s.Format == "int64" {
			return "int64"
		}
		return "int"
	case "number":
		if s.Format == "float" {
			return "float32"
		}
		return "float64"
	case "boolean":
		return "bool"
	case "string":
		return "string"
	case "array":
		return "[]interface{}"
	case "object":
		return "map[string]interface{}"
	default:
		return "interface{}"
	}
}

// pathToGoLiteral converts "/users/{id}" to `"/users/{id}"` (unchanged).
// The path params will be substituted at runtime via WithPathParam.
func pathLiteral(path string, params []paramData) string {
	return `"` + path + `"`
}

// httpMethodFunc returns the Client method name for an HTTP verb.
func httpMethodFunc(method string) string {
	switch strings.ToUpper(method) {
	case "GET":
		return "Get"
	case "POST":
		return "Post"
	case "PUT":
		return "Put"
	case "PATCH":
		return "Patch"
	case "DELETE":
		return "Delete"
	default:
		return "Get"
	}
}

// formatQueryParam produces the expression to convert a Go value to string.
func formatQueryParam(goType, goName string) string {
	switch goType {
	case "*int", "*int64", "*int32":
		base := strings.TrimPrefix(goType, "*")
		return fmt.Sprintf(`fmt.Sprintf("%%d", *%s)`, goName) + " /* " + base + " */"
	case "*float64", "*float32":
		return fmt.Sprintf(`fmt.Sprintf("%%g", *%s)`, goName)
	case "*bool":
		return fmt.Sprintf(`fmt.Sprintf("%%t", *%s)`, goName)
	case "string":
		return goName
	default:
		if strings.HasPrefix(goType, "*") {
			return fmt.Sprintf(`fmt.Sprintf("%%v", *%s)`, goName)
		}
		return fmt.Sprintf(`fmt.Sprintf("%%v", %s)`, goName)
	}
}

// hasFmtUsage checks whether any param needs fmt.Sprintf.
func hasFmtUsage(methods []methodData) bool {
	for _, m := range methods {
		for _, p := range m.Params {
			if p.Kind == "query" && p.GoType != "string" {
				return true
			}
		}
	}
	return false
}

// ── Parser ───────────────────────────────────────────────────────────────────

func parseSpec(data []byte) (*openAPISpec, error) {
	var spec openAPISpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing OpenAPI spec: %w", err)
	}
	return &spec, nil
}

func buildClientData(spec *openAPISpec, pkg, module string) clientData {
	var methods []methodData

	// Sort paths for deterministic output.
	paths := make([]string, 0, len(spec.Paths))
	for p := range spec.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		item := spec.Paths[path]
		type opEntry struct {
			method string
			op     *operation
		}
		ops := []opEntry{
			{"GET", item.Get},
			{"POST", item.Post},
			{"PUT", item.Put},
			{"PATCH", item.Patch},
			{"DELETE", item.Delete},
		}
		for _, entry := range ops {
			if entry.op == nil {
				continue
			}
			op := entry.op
			funcName := pascalCase(op.OperationID)
			if funcName == "" {
				// Fallback: method + sanitised path.
				funcName = pascalCase(entry.method + "_" + strings.ReplaceAll(strings.Trim(path, "/"), "/", "_"))
			}

			var params []paramData
			hasQuery := false
			hasPath := false
			for _, p := range op.Parameters {
				gt := schemaGoType(p.Schema)
				optional := !p.Required
				if p.In == "query" && optional {
					gt = "*" + gt
				}
				pd := paramData{
					GoName:   sanitizeIdent(p.Name),
					APIName:  p.Name,
					Kind:     p.In,
					GoType:   gt,
					Optional: optional,
				}
				params = append(params, pd)
				if p.In == "query" {
					hasQuery = true
				}
				if p.In == "path" {
					hasPath = true
				}
			}

			hasBody := op.RequestBody != nil
			methods = append(methods, methodData{
				FuncName:   funcName,
				HTTPMethod: entry.method,
				Path:       path,
				Params:     params,
				HasBody:    hasBody,
				HasQuery:   hasQuery,
				HasPath:    hasPath,
			})
		}
	}

	return clientData{
		Package: pkg,
		Module:  module,
		Methods: methods,
		HasFmt:  hasFmtUsage(methods),
	}
}

func buildModelsData(spec *openAPISpec, pkg string) modelsData {
	names := make([]string, 0, len(spec.Components.Schemas))
	for n := range spec.Components.Schemas {
		names = append(names, n)
	}
	sort.Strings(names)

	schemas := make([]schemaData, 0, len(names))

	for _, name := range names {
		s := spec.Components.Schemas[name]

		fieldNames := make([]string, 0, len(s.Properties))
		for fn := range s.Properties {
			fieldNames = append(fieldNames, fn)
		}
		sort.Strings(fieldNames)

		fields := make([]fieldData, 0, len(fieldNames))

		for _, fn := range fieldNames {
			fp := s.Properties[fn]
			fields = append(fields, fieldData{
				Name:    pascalCase(fn),
				JSONTag: fn,
				GoType:  schemaGoType(fp),
			})
		}
		schemas = append(schemas, schemaData{
			Name:   pascalCase(name),
			Fields: fields,
		})
	}
	return modelsData{Package: pkg, Schemas: schemas}
}

// sanitizeIdent turns names like "user-id" into valid Go identifiers.
func sanitizeIdent(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	// Lowercase first char for parameter names.
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// ── Code rendering ────────────────────────────────────────────────────────────

var tmplFuncs = template.FuncMap{
	"httpMethodFunc":   httpMethodFunc,
	"pathLiteral":      pathLiteral,
	"formatQueryParam": formatQueryParam,
}

func renderClient(data clientData) ([]byte, error) {
	tmpl, err := template.New("client").Funcs(tmplFuncs).Parse(clientTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

func renderModels(data modelsData) ([]byte, error) {
	tmpl, err := template.New("models").Funcs(tmplFuncs).Parse(modelsTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return format.Source(buf.Bytes())
}

// ── Generator entry point ────────────────────────────────────────────────────

// GenerateResult holds the generated file contents keyed by filename.
type GenerateResult struct {
	Files map[string][]byte
}

// Generate parses the OpenAPI JSON spec and returns generated Go source files.
func Generate(specData []byte, pkg, module string) (*GenerateResult, error) {
	spec, err := parseSpec(specData)
	if err != nil {
		return nil, err
	}

	clientSrc, err := renderClient(buildClientData(spec, pkg, module))
	if err != nil {
		return nil, fmt.Errorf("rendering client: %w", err)
	}

	modelsSrc, err := renderModels(buildModelsData(spec, pkg))
	if err != nil {
		return nil, fmt.Errorf("rendering models: %w", err)
	}

	return &GenerateResult{
		Files: map[string][]byte{
			"client.go": clientSrc,
			"models.go": modelsSrc,
		},
	}, nil
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	input := flag.String("input", "", "Path to OpenAPI 3.x spec (JSON or YAML)")
	output := flag.String("output", "./generated", "Output directory for generated files")
	pkg := flag.String("package", "client", "Go package name for generated code")
	dryRun := flag.Bool("dry-run", false, "Print generated code to stdout instead of writing files")
	module := flag.String("module", "github.com/jhonsferg/relay", "Module path for relay import")
	flag.Parse()

	if *input == "" {
		fmt.Fprintln(os.Stderr, "relay-gen: -input is required")
		flag.Usage()
		os.Exit(1)
	}

	// Detect YAML by extension.
	lower := strings.ToLower(*input)
	if strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml") {
		fmt.Fprintln(os.Stderr, "relay-gen: YAML not supported, convert to JSON first (e.g. yq . spec.yaml > spec.json)")
		os.Exit(1)
	}

	data, err := os.ReadFile(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay-gen: reading input: %v\n", err)
		os.Exit(1)
	}

	result, err := Generate(data, *pkg, *module)
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay-gen: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		// Print files in deterministic order.
		names := make([]string, 0, len(result.Files))
		for n := range result.Files {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Printf("// === %s ===\n%s\n", name, result.Files[name])
		}
		return
	}

	if err := os.MkdirAll(*output, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "relay-gen: creating output dir: %v\n", err)
		os.Exit(1)
	}

	for name, src := range result.Files {
		dest := filepath.Join(*output, name)
		if err := os.WriteFile(dest, src, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "relay-gen: writing %s: %v\n", dest, err)
			os.Exit(1)
		}
		fmt.Printf("relay-gen: wrote %s\n", dest)
	}
}
