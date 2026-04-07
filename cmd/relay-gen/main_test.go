package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const simpleGetSpec = `{
  "paths": {
    "/users": {
      "get": {
        "operationId": "listUsers",
        "parameters": [
          {"name": "limit", "in": "query", "schema": {"type": "integer"}}
        ],
        "responses": {"200": {"description": "success"}}
      }
    }
  },
  "components": {"schemas": {}}
}`

const pathParamSpec = `{
  "paths": {
    "/users/{id}": {
      "get": {
        "operationId": "getUser",
        "parameters": [
          {"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {"200": {"description": "success"}}
      }
    }
  },
  "components": {"schemas": {}}
}`

const postWithBodySpec = `{
  "paths": {
    "/users": {
      "post": {
        "operationId": "createUser",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {"$ref": "#/components/schemas/User"}
            }
          }
        },
        "responses": {"201": {"description": "created"}}
      }
    }
  },
  "components": {
    "schemas": {
      "User": {
        "type": "object",
        "properties": {
          "name": {"type": "string"},
          "age":  {"type": "integer"}
        }
      }
    }
  }
}`

func TestGenerate_SimpleGet(t *testing.T) {
	result, err := Generate([]byte(simpleGetSpec), "myclient", "github.com/jhonsferg/relay")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	clientSrc, ok := result.Files["client.go"]
	if !ok {
		t.Fatal("expected client.go in result")
	}
	src := string(clientSrc)
	if !strings.Contains(src, "func (c *Client) ListUsers(") {
		t.Errorf("expected ListUsers function, got:\n%s", src)
	}
	if !strings.Contains(src, `c.inner.Get("/users")`) {
		t.Errorf("expected Get(\"/users\"), got:\n%s", src)
	}
}

func TestGenerate_PathParams(t *testing.T) {
	result, err := Generate([]byte(pathParamSpec), "myclient", "github.com/jhonsferg/relay")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	src := string(result.Files["client.go"])
	if !strings.Contains(src, "func (c *Client) GetUser(") {
		t.Errorf("expected GetUser function, got:\n%s", src)
	}
	if !strings.Contains(src, "id string") {
		t.Errorf("expected id string param, got:\n%s", src)
	}
	if !strings.Contains(src, `WithPathParam("id", id)`) {
		t.Errorf("expected WithPathParam call, got:\n%s", src)
	}
}

func TestGenerate_PostWithBody(t *testing.T) {
	result, err := Generate([]byte(postWithBodySpec), "myclient", "github.com/jhonsferg/relay")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	src := string(result.Files["client.go"])
	if !strings.Contains(src, "func (c *Client) CreateUser(") {
		t.Errorf("expected CreateUser function, got:\n%s", src)
	}
	if !strings.Contains(src, "body interface{}") {
		t.Errorf("expected body interface{} param, got:\n%s", src)
	}
	if !strings.Contains(src, "WithJSON(body)") {
		t.Errorf("expected WithJSON call, got:\n%s", src)
	}
}

func TestGenerate_DryRun(t *testing.T) {
	// Write spec to a temp file in the test's temp dir.
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specFile, []byte(simpleGetSpec), 0o600); err != nil {
		t.Fatalf("writing spec: %v", err)
	}

	// Capture stdout by redirecting os.Stdout.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	os.Stdout = w

	// Run the same logic as main's dry-run: generate then print.
	data, err := os.ReadFile(specFile) //nolint:gosec // specFile is a test-controlled path
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	result, err := Generate(data, "myclient", "github.com/jhonsferg/relay")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for name, src := range result.Files {
		_, _ = w.WriteString("// === " + name + " ===\n" + string(src) + "\n")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe: %v", err)
	}
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("reading output: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "client.go") {
		t.Errorf("expected client.go in dry-run output, got:\n%s", out)
	}
	if !strings.Contains(out, "ListUsers") {
		t.Errorf("expected ListUsers in dry-run output, got:\n%s", out)
	}
}

func TestGenerate_Models(t *testing.T) {
	result, err := Generate([]byte(postWithBodySpec), "myclient", "github.com/jhonsferg/relay")
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	src := string(result.Files["models.go"])
	if !strings.Contains(src, "type User struct") {
		t.Errorf("expected User struct in models.go, got:\n%s", src)
	}
	if !strings.Contains(src, `json:"name"`) {
		t.Errorf("expected json tag for name, got:\n%s", src)
	}
}

func TestPascalCase(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"listUsers", "ListUsers"},
		{"get_user", "GetUser"},
		{"createUser", "CreateUser"},
		{"delete-item", "DeleteItem"},
		{"getHTTPResponse", "GetHTTPResponse"},
		{"simple", "Simple"},
		{"", ""},
	}
	for _, tc := range tests {
		got := pascalCase(tc.input)
		if got != tc.want {
			t.Errorf("pascalCase(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
