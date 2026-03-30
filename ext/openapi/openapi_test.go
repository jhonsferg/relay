package openapi_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jhonsferg/relay"
	relayopenapi "github.com/jhonsferg/relay/ext/openapi"
)

// petStoreSpec is a minimal OpenAPI 3.0 spec for testing.
const petStoreSpec = `
openapi: "3.0.0"
info:
  title: Pet Store
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /pets:
    get:
      operationId: listPets
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        "200":
          description: list of pets
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: "#/components/schemas/Pet"
    post:
      operationId: createPet
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/NewPet"
      responses:
        "201":
          description: pet created
  /pets/{id}:
    get:
      operationId: getPet
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: a pet
components:
  schemas:
    Pet:
      type: object
      required: [id, name]
      properties:
        id:
          type: integer
        name:
          type: string
    NewPet:
      type: object
      required: [name]
      properties:
        name:
          type: string
`

func newTestClient(t *testing.T, srv *httptest.Server, opts ...relayopenapi.Option) *relay.Client {
	t.Helper()
	doc, err := relayopenapi.Load([]byte(petStoreSpec))
	if err != nil {
		t.Fatalf("Load spec: %v", err)
	}
	return relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relayopenapi.WithValidation(doc, opts...),
	)
}

func TestLoad_Valid(t *testing.T) {
	_, err := relayopenapi.Load([]byte(petStoreSpec))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestLoad_Invalid(t *testing.T) {
	_, err := relayopenapi.Load([]byte("not: valid: openapi"))
	if err == nil {
		t.Fatal("expected error for invalid spec, got nil")
	}
}

func TestRequestValidation_ValidGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	resp, err := client.Execute(client.Get("/pets"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestRequestValidation_InvalidBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)

	// Missing required "name" field.
	body := strings.NewReader(`{"age": 3}`)
	req := client.Post("/pets").
		WithHeader("Content-Type", "application/json").
		WithBodyReader(body)

	_, err := client.Execute(req)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}

	ve, ok := relayopenapi.IsValidationError(err)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Phase != "request" {
		t.Errorf("phase = %q, want %q", ve.Phase, "request")
	}
}

func TestRequestValidation_UnknownRoute_Passthrough(t *testing.T) {
	var serverCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newTestClient(t, srv)
	// /unknown is not in the spec — should pass through.
	resp, err := client.Execute(client.Get("/unknown"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !serverCalled {
		t.Error("expected server to be called for unknown route")
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestResponseValidation_ValidResponse(t *testing.T) {
	pets := []map[string]interface{}{{"id": 1, "name": "Fido"}}
	body, _ := json.Marshal(pets)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	}))
	defer srv.Close()

	client := newTestClient(t, srv, relayopenapi.WithResponseValidation())
	resp, err := client.Execute(client.Get("/pets"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestResponseValidation_InvalidResponse(t *testing.T) {
	// Missing required "id" field in response.
	body := `[{"name":"Fido"}]`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	client := newTestClient(t, srv, relayopenapi.WithResponseValidation())
	_, err := client.Execute(client.Get("/pets"))
	if err == nil {
		t.Fatal("expected validation error for invalid response, got nil")
	}

	ve, ok := relayopenapi.IsValidationError(err)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Phase != "response" {
		t.Errorf("phase = %q, want %q", ve.Phase, "response")
	}
}

func TestIsValidationError(t *testing.T) {
	ve := &relayopenapi.ValidationError{Phase: "request", Cause: errors.New("bad")}
	wrapped := errors.New("wrap: " + ve.Error())
	_ = wrapped

	got, ok := relayopenapi.IsValidationError(ve)
	if !ok {
		t.Fatal("IsValidationError should return true for *ValidationError")
	}
	if got != ve {
		t.Error("IsValidationError should return the original *ValidationError")
	}

	_, ok = relayopenapi.IsValidationError(errors.New("unrelated"))
	if ok {
		t.Error("IsValidationError should return false for unrelated errors")
	}
}

func TestValidationError_Error(t *testing.T) {
	ve := &relayopenapi.ValidationError{
		Phase: "request",
		Cause: errors.New("missing field"),
	}
	got := ve.Error()
	if !strings.Contains(got, "request") {
		t.Errorf("Error() = %q, want it to contain %q", got, "request")
	}
	if !strings.Contains(got, "missing field") {
		t.Errorf("Error() = %q, want it to contain %q", got, "missing field")
	}

	if unwrapped := errors.Unwrap(ve); unwrapped != ve.Cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, ve.Cause)
	}
}
