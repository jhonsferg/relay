package relay

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jhonsferg/relay/testutil"
)

// --- StructValidator tests ---

type validStruct struct {
	ID   int    `json:"id"   validate:"required"`
	Name string `json:"name" validate:"required,min=1"`
}

func TestStructValidator_PassesValid(t *testing.T) {
	sv := NewStructValidator(validStruct{})
	input := map[string]interface{}{
		"id":   float64(42),
		"name": "Alice",
	}
	if err := sv.Validate(input); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestStructValidator_FailsMissingRequired(t *testing.T) {
	sv := NewStructValidator(validStruct{})
	// id is missing (zero value)
	input := map[string]interface{}{
		"name": "Alice",
	}
	err := sv.Validate(input)
	if err == nil {
		t.Fatal("expected validation error for missing required field, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "id" {
		t.Errorf("expected field 'id', got %q", ve.Field)
	}
}

func TestStructValidator_MinLength(t *testing.T) {
	sv := NewStructValidator(validStruct{})
	// name is present but empty (length 0, min=1)
	input := map[string]interface{}{
		"id":   float64(1),
		"name": "",
	}
	err := sv.Validate(input)
	if err == nil {
		t.Fatal("expected validation error for min length violation, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

// --- JSONSchemaValidator tests ---

func TestJSONSchemaValidator_TypeCheck(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"type":"object","required":["id"]}`)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}
	input := map[string]interface{}{"id": float64(1)}
	if err := jv.Validate(input); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestJSONSchemaValidator_MissingRequired(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"type":"object","required":["id"]}`)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}
	input := map[string]interface{}{}
	err = jv.Validate(input)
	if err == nil {
		t.Fatal("expected validation error for missing required field, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}

// --- Integration test ---

type integrationResponse struct {
	Status string `json:"status" validate:"required"`
}

func TestWithResponseValidator_Integration(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Test: valid response passes validation
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"status":"ok"}`,
	})

	client := New(
		WithBaseURL(srv.URL()),
		WithResponseValidator(NewStructValidator(integrationResponse{})),
	)

	resp, err := client.Execute(client.Get("/test"))
	if err != nil {
		t.Fatalf("expected no error for valid response, got: %v", err)
	}
	if resp == nil || !resp.IsSuccess() {
		t.Fatal("expected successful response")
	}

	// Test: invalid response (missing required field) returns ValidationError
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{}`,
	})

	_, err = client.Execute(client.Get("/test"))
	if err == nil {
		t.Fatal("expected validation error for bad response, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}
