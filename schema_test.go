package relay

import (
	"errors"
	"net/http"
	"reflect"
	"sync"
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

func TestValidationError_Error(t *testing.T) {
	withField := &ValidationError{Field: "name", Message: "is required"}
	if got := withField.Error(); got != `validation error: field "name": is required` {
		t.Errorf("Error() = %q", got)
	}
	withoutField := &ValidationError{Message: "malformed"}
	if got := withoutField.Error(); got != "validation error: malformed" {
		t.Errorf("Error() = %q", got)
	}
}

// --- applyRule / StructValidator: max=, numeric kinds, malformed rules ---

type maxRuleStruct struct {
	Name  string  `json:"name" validate:"max=3"`
	Count int     `json:"count" validate:"max=10"`
	UCnt  uint    `json:"ucnt" validate:"max=10"`
	Rate  float64 `json:"rate" validate:"max=1.5"`
}

func TestStructValidator_MaxRule_AllKinds(t *testing.T) {
	sv := NewStructValidator(maxRuleStruct{})
	tests := []struct {
		name    string
		input   map[string]interface{}
		wantErr bool
	}{
		{"all within max", map[string]interface{}{"name": "ab", "count": float64(5), "ucnt": float64(5), "rate": 1.0}, false},
		{"string exceeds max", map[string]interface{}{"name": "abcd", "count": float64(5), "ucnt": float64(5), "rate": 1.0}, true},
		{"int exceeds max", map[string]interface{}{"name": "ab", "count": float64(11), "ucnt": float64(5), "rate": 1.0}, true},
		{"uint exceeds max", map[string]interface{}{"name": "ab", "count": float64(5), "ucnt": float64(11), "rate": 1.0}, true},
		{"float exceeds max", map[string]interface{}{"name": "ab", "count": float64(5), "ucnt": float64(5), "rate": 2.0}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := sv.Validate(tc.input)
			if tc.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

type minIntUintFloatStruct struct {
	Count int     `json:"count" validate:"min=10"`
	UCnt  uint    `json:"ucnt" validate:"min=10"`
	Rate  float64 `json:"rate" validate:"min=1.5"`
}

func TestStructValidator_MinRule_IntUintFloatKinds(t *testing.T) {
	sv := NewStructValidator(minIntUintFloatStruct{})
	tests := []struct {
		name    string
		input   map[string]interface{}
		wantErr bool
	}{
		{"all above min", map[string]interface{}{"count": float64(20), "ucnt": float64(20), "rate": 2.0}, false},
		{"int below min", map[string]interface{}{"count": float64(1), "ucnt": float64(20), "rate": 2.0}, true},
		{"uint below min", map[string]interface{}{"count": float64(20), "ucnt": float64(1), "rate": 2.0}, true},
		{"float below min", map[string]interface{}{"count": float64(20), "ucnt": float64(20), "rate": 0.1}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := sv.Validate(tc.input)
			if tc.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestApplyRule_MalformedMinMaxRule(t *testing.T) {
	fv := reflect.ValueOf("x")
	if err := applyRule("min=notanumber", "f", fv); err == nil {
		t.Error("expected error for malformed min rule")
	}
	if err := applyRule("max=notanumber", "f", fv); err == nil {
		t.Error("expected error for malformed max rule")
	}
	// Unknown rule is a silent no-op.
	if err := applyRule("unknown-rule", "f", fv); err != nil {
		t.Errorf("expected no error for unrecognised rule, got: %v", err)
	}
}

func TestStructValidator_NonStructPrototype(t *testing.T) {
	sv := NewStructValidator(42) // not a struct
	err := sv.Validate(map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for non-struct prototype")
	}
}

func TestStructValidator_UnmarshalableValue(t *testing.T) {
	sv := NewStructValidator(validStruct{})
	// A channel cannot be marshalled to JSON.
	err := sv.Validate(make(chan int))
	if err == nil {
		t.Fatal("expected error when the value cannot be re-encoded to JSON")
	}
}

// --- checkJSONType: every branch ---

func TestCheckJSONType_AllVariants(t *testing.T) {
	tests := []struct {
		name    string
		v       interface{}
		typStr  string
		wantErr bool
	}{
		{"object matches", map[string]interface{}{}, "object", false},
		{"array matches", []interface{}{}, "array", false},
		{"string matches", "s", "string", false},
		{"boolean matches", true, "boolean", false},
		{"number matches", float64(1), "number", false},
		{"null matches", nil, "null", false},
		{"integer subtype matches whole float64", float64(5), "integer", false},
		{"integer subtype rejects fractional float64", float64(5.5), "integer", true},
		{"integer subtype rejects non-number", "5", "integer", true},
		{"mismatched type", "s", "number", true},
		{"unknown go type", struct{}{}, "object", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := checkJSONType(tc.v, tc.typStr, "field")
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

// --- toFloat64 / toNumber: every branch, including the zero-value default ---

func TestToFloat64_AllVariants(t *testing.T) {
	tests := []struct {
		name string
		v    interface{}
		want float64
	}{
		{"float64", float64(3.5), 3.5},
		{"int", int(7), 7},
		{"int64", int64(9), 9},
		{"unsupported type defaults to zero", "not a number", 0},
		{"nil defaults to zero", nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := toFloat64(tc.v); got != tc.want {
				t.Errorf("toFloat64(%v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}

func TestToNumber_AllVariants(t *testing.T) {
	if n, ok := toNumber(float64(3.5)); !ok || n != 3.5 {
		t.Errorf("toNumber(float64) = %v, %v; want 3.5, true", n, ok)
	}
	if n, ok := toNumber(int(7)); !ok || n != 7 {
		t.Errorf("toNumber(int) = %v, %v; want 7, true", n, ok)
	}
	if _, ok := toNumber("not a number"); ok {
		t.Error("toNumber(string) should return ok=false")
	}
	if _, ok := toNumber(nil); ok {
		t.Error("toNumber(nil) should return ok=false")
	}
}

// --- JSON Schema: string/numeric keywords, nested properties, malformed pattern ---

func TestJSONSchemaValidator_StringLengthKeywords(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"type":"string","minLength":2,"maxLength":5}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}
	if err := jv.Validate("ab"); err != nil {
		t.Errorf("expected no error for in-range length, got: %v", err)
	}
	if err := jv.Validate("a"); err == nil {
		t.Error("expected error for string shorter than minLength")
	}
	if err := jv.Validate("abcdef"); err == nil {
		t.Error("expected error for string longer than maxLength")
	}
}

func TestJSONSchemaValidator_Pattern(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"type":"string","pattern":"^[a-z]+$"}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}
	if err := jv.Validate("abc"); err != nil {
		t.Errorf("expected match, got: %v", err)
	}
	if err := jv.Validate("ABC"); err == nil {
		t.Error("expected pattern mismatch error")
	}
}

func TestJSONSchemaValidator_InvalidPattern(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"type":"string","pattern":"[invalid("}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}
	if err := jv.Validate("abc"); err == nil {
		t.Error("expected error for an invalid regex pattern")
	}
	// The compile error itself must also be cached (not just successful
	// compiles) - repeat validation to exercise the cached-error path.
	if err := jv.Validate("abc"); err == nil {
		t.Error("expected the invalid-pattern error on the second call too")
	}
}

// TestJSONSchemaValidator_PatternCacheReusedAcrossCalls guards against a
// regression where the "pattern" keyword was recompiled via
// regexp.MatchString on every single Validate call - regexp compilation is
// far more expensive than matching, and the pattern is static once the
// validator is constructed, so recompiling on every call is pure waste that
// scales with how often responses are validated. This test can't observe
// compilation cost directly, but it does confirm correctness is preserved
// once compiled results are cached and reused across many calls with
// different input values.
func TestJSONSchemaValidator_PatternCacheReusedAcrossCalls(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"type":"string","pattern":"^[a-z]+$"}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}
	for i := 0; i < 50; i++ {
		if err := jv.Validate("abc"); err != nil {
			t.Fatalf("iteration %d: expected match, got: %v", i, err)
		}
		if err := jv.Validate("ABC"); err == nil {
			t.Fatalf("iteration %d: expected mismatch error", i)
		}
	}
}

// TestJSONSchemaValidator_PatternCacheConcurrentSafe exercises the pattern
// cache (a sync.Map) from many goroutines concurrently, since a validator is
// typically shared across a client used by multiple goroutines.
func TestJSONSchemaValidator_PatternCacheConcurrentSafe(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"type":"string","pattern":"^[a-z]+$"}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				_ = jv.Validate("abc")
				_ = jv.Validate("ABC")
			}
		}()
	}
	wg.Wait()
}

// BenchmarkJSONSchemaValidator_Pattern measures repeated Validate calls
// against a schema with a "pattern" keyword - the regex compile-vs-cache
// difference should show up directly in ns/op and allocs/op here.
func BenchmarkJSONSchemaValidator_Pattern(b *testing.B) {
	jv, err := NewJSONSchemaValidator(`{"type":"string","pattern":"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"}`)
	if err != nil {
		b.Fatalf("NewJSONSchemaValidator: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := jv.Validate("user@example.com"); err != nil {
			b.Fatal(err)
		}
	}
}

func TestJSONSchemaValidator_MinimumMaximum(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"type":"number","minimum":1,"maximum":10}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}
	if err := jv.Validate(float64(5)); err != nil {
		t.Errorf("expected no error for in-range number, got: %v", err)
	}
	if err := jv.Validate(float64(0)); err == nil {
		t.Error("expected error for number below minimum")
	}
	if err := jv.Validate(float64(11)); err == nil {
		t.Error("expected error for number above maximum")
	}
}

func TestJSONSchemaValidator_NestedProperties(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"required": ["name"],
				"properties": {
					"name": {"type": "string", "minLength": 1}
				}
			}
		}
	}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}

	valid := map[string]interface{}{"user": map[string]interface{}{"name": "Alice"}}
	if err := jv.Validate(valid); err != nil {
		t.Errorf("expected no error for valid nested object, got: %v", err)
	}

	missingNested := map[string]interface{}{"user": map[string]interface{}{}}
	if err := jv.Validate(missingNested); err == nil {
		t.Error("expected error for missing required nested field")
	}

	wrongType := map[string]interface{}{"user": map[string]interface{}{"name": 42}}
	if err := jv.Validate(wrongType); err == nil {
		t.Error("expected error for wrong nested field type")
	}
}

// TestJSONSchemaValidator_OptionalTypedPropertyAbsent guards against an
// absent (not "required") typed property being validated as if the object
// had explicitly set it to JSON null, failing "expected type X, got null"
// for a field that was simply omitted - making every typed property
// effectively mandatory regardless of the schema's "required" list.
func TestJSONSchemaValidator_OptionalTypedPropertyAbsent(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		}
	}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}

	// "name" is not in "required" and is entirely absent - must be valid.
	if err := jv.Validate(map[string]interface{}{}); err != nil {
		t.Errorf("expected no error for an absent optional typed property, got: %v", err)
	}

	// Present with the correct type still validates normally.
	if err := jv.Validate(map[string]interface{}{"name": "Alice"}); err != nil {
		t.Errorf("expected no error for a correctly-typed present property, got: %v", err)
	}

	// Present with the wrong type must still fail.
	if err := jv.Validate(map[string]interface{}{"name": 42}); err == nil {
		t.Error("expected error for a present property with the wrong type")
	}
}

func TestJSONSchemaValidator_RequiredOnNonObject(t *testing.T) {
	jv, err := NewJSONSchemaValidator(`{"required":["id"]}`)
	if err != nil {
		t.Fatalf("NewJSONSchemaValidator: %v", err)
	}
	if err := jv.Validate("not an object"); err == nil {
		t.Error("expected error when required is checked against a non-object value")
	}
}

func TestNewJSONSchemaValidator_InvalidJSON(t *testing.T) {
	_, err := NewJSONSchemaValidator(`{not valid json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON schema")
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
