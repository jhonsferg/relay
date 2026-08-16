package relay

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// SchemaValidator validates a decoded JSON response against constraints.
type SchemaValidator interface {
	// Validate checks the decoded value and returns an error if validation fails.
	// The value is the result of json.Unmarshal into an interface{}.
	Validate(v interface{}) error
}

// ValidationError is returned when the response body fails schema validation.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error: field %q: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

// StructValidator validates a response by attempting to decode it into
// the provided struct type and checking required fields via struct tags.
//
// Usage:
//
//	type MyResponse struct {
//	    ID   int    `json:"id"   validate:"required"`
//	    Name string `json:"name" validate:"required,min=1"`
//	}
//	relay.WithResponseValidator(relay.NewStructValidator(MyResponse{}))
type StructValidator struct {
	prototype interface{}
}

// NewStructValidator creates a StructValidator using the given struct as the
// validation template. prototype must be a struct value or pointer to struct.
func NewStructValidator(prototype interface{}) *StructValidator {
	return &StructValidator{prototype: prototype}
}

// Validate decodes v (which must be a map[string]interface{}) into the
// prototype struct and checks validate tags:
//   - "required": field must be non-zero
//   - "min=N": string min length / number min value
//   - "max=N": string max length / number max value
func (s *StructValidator) Validate(v interface{}) error {
	// Re-encode v to JSON and decode into a fresh copy of the prototype.
	raw, err := json.Marshal(v)
	if err != nil {
		return &ValidationError{Message: fmt.Sprintf("cannot re-encode value: %s", err)}
	}

	t := reflect.TypeOf(s.prototype)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return &ValidationError{Message: "prototype must be a struct"}
	}

	target := reflect.New(t)
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return &ValidationError{Message: fmt.Sprintf("cannot decode into prototype: %s", err)}
	}

	return validateStruct(target.Elem())
}

func validateStruct(rv reflect.Value) error {
	rt := rv.Type()
	for i := range rt.NumField() {
		field := rt.Field(i)
		fv := rv.Field(i)

		tag := field.Tag.Get("validate")
		if tag == "" {
			continue
		}

		jsonName := field.Tag.Get("json")
		if jsonName == "" {
			jsonName = field.Name
		} else {
			jsonName = strings.Split(jsonName, ",")[0]
		}

		rules := strings.Split(tag, ",")
		for _, rule := range rules {
			rule = strings.TrimSpace(rule)
			if err := applyRule(rule, jsonName, fv); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyRule(rule, fieldName string, fv reflect.Value) error {
	switch {
	case rule == "required":
		if fv.IsZero() {
			return &ValidationError{Field: fieldName, Message: "required field is missing or zero"}
		}

	case strings.HasPrefix(rule, "min="):
		n, err := strconv.ParseFloat(strings.TrimPrefix(rule, "min="), 64)
		if err != nil {
			return &ValidationError{Field: fieldName, Message: fmt.Sprintf("invalid min rule: %s", rule)}
		}
		switch fv.Kind() { //nolint:exhaustive
		case reflect.String:
			if float64(len(fv.String())) < n {
				return &ValidationError{Field: fieldName, Message: fmt.Sprintf("length %d is less than minimum %g", len(fv.String()), n)}
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if float64(fv.Int()) < n {
				return &ValidationError{Field: fieldName, Message: fmt.Sprintf("value %d is less than minimum %g", fv.Int(), n)}
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if float64(fv.Uint()) < n {
				return &ValidationError{Field: fieldName, Message: fmt.Sprintf("value %d is less than minimum %g", fv.Uint(), n)}
			}
		case reflect.Float32, reflect.Float64:
			if fv.Float() < n {
				return &ValidationError{Field: fieldName, Message: fmt.Sprintf("value %g is less than minimum %g", fv.Float(), n)}
			}
		}

	case strings.HasPrefix(rule, "max="):
		n, err := strconv.ParseFloat(strings.TrimPrefix(rule, "max="), 64)
		if err != nil {
			return &ValidationError{Field: fieldName, Message: fmt.Sprintf("invalid max rule: %s", rule)}
		}
		switch fv.Kind() { //nolint:exhaustive
		case reflect.String:
			if float64(len(fv.String())) > n {
				return &ValidationError{Field: fieldName, Message: fmt.Sprintf("length %d exceeds maximum %g", len(fv.String()), n)}
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if float64(fv.Int()) > n {
				return &ValidationError{Field: fieldName, Message: fmt.Sprintf("value %d exceeds maximum %g", fv.Int(), n)}
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if float64(fv.Uint()) > n {
				return &ValidationError{Field: fieldName, Message: fmt.Sprintf("value %d exceeds maximum %g", fv.Uint(), n)}
			}
		case reflect.Float32, reflect.Float64:
			if fv.Float() > n {
				return &ValidationError{Field: fieldName, Message: fmt.Sprintf("value %g exceeds maximum %g", fv.Float(), n)}
			}
		}
	}
	return nil
}

// JSONSchemaValidator validates a response against a minimal JSON Schema subset.
// Supported keywords: type, required, properties, minLength, maxLength, minimum, maximum, pattern.
type JSONSchemaValidator struct {
	schema map[string]interface{}

	// patternCache holds compiled "pattern" keyword regexes, keyed by the
	// pattern string, populated lazily on first use. The schema is immutable
	// after construction, so cached patterns never go stale. Without this,
	// every response field validated against a "pattern" keyword recompiled
	// the regex from scratch on every single Validate call - regexp
	// compilation costs far more than matching, and it's avoidable entirely
	// since the same pattern strings repeat across calls for a given
	// validator. sync.Map since Validate must be safe for concurrent use
	// (the same validator is typically shared across a client used by
	// multiple goroutines).
	patternCache sync.Map
}

// NewJSONSchemaValidator creates a JSONSchemaValidator from a JSON Schema string.
func NewJSONSchemaValidator(schemaJSON string) (*JSONSchemaValidator, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return nil, fmt.Errorf("invalid JSON schema: %w", err)
	}
	return &JSONSchemaValidator{schema: schema}, nil
}

// Validate validates v against the JSON Schema.
func (j *JSONSchemaValidator) Validate(v interface{}) error {
	return j.validateJSONSchema(v, j.schema, "")
}

// compiledPattern returns a compiled regexp for pat, using patternCache to
// avoid recompiling the same pattern string across repeated Validate calls.
func (j *JSONSchemaValidator) compiledPattern(pat string) (*regexp.Regexp, error) {
	if cached, ok := j.patternCache.Load(pat); ok {
		if err, isErr := cached.(error); isErr {
			return nil, err
		}
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		j.patternCache.Store(pat, err)
		return nil, err
	}
	j.patternCache.Store(pat, re)
	return re, nil
}

func (j *JSONSchemaValidator) validateJSONSchema(v interface{}, schema map[string]interface{}, path string) error {
	fieldLabel := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}

	// type check
	if typVal, ok := schema["type"]; ok {
		typStr, _ := typVal.(string)
		if err := checkJSONType(v, typStr, path); err != nil {
			return err
		}
	}

	obj, isObj := v.(map[string]interface{})

	// required
	if reqVal, ok := schema["required"]; ok {
		reqList, _ := reqVal.([]interface{})
		for _, r := range reqList {
			key, _ := r.(string)
			if isObj {
				if _, present := obj[key]; !present {
					return &ValidationError{Field: fieldLabel(key), Message: "required field is missing"}
				}
			} else {
				return &ValidationError{Field: fieldLabel(key), Message: "required field is missing (response is not an object)"}
			}
		}
	}

	// properties
	if propsVal, ok := schema["properties"]; ok {
		props, _ := propsVal.(map[string]interface{})
		for propName, propSchemaRaw := range props {
			propSchema, _ := propSchemaRaw.(map[string]interface{})
			if propSchema == nil {
				continue
			}
			// A property absent from the object (indexing a nil map, when v
			// isn't even an object, also reports !present) is only an error
			// if it's listed in "required" - already checked above. An
			// absent optional property has nothing to validate: without
			// this check, propVal defaulted to nil/zero value and got
			// validated as if the object had explicitly set the field to
			// JSON null, failing any type-checked ("string", "integer",
			// etc.) optional property that was simply omitted - making
			// every typed property effectively mandatory regardless of
			// "required".
			propVal, present := obj[propName]
			if !present {
				continue
			}
			if err := j.validateJSONSchema(propVal, propSchema, fieldLabel(propName)); err != nil {
				return err
			}
		}
	}

	// String-level keywords
	if strVal, ok := v.(string); ok {
		if minLen, ok := schema["minLength"]; ok {
			n := toFloat64(minLen)
			if float64(len(strVal)) < n {
				return &ValidationError{Field: path, Message: fmt.Sprintf("length %d is less than minLength %g", len(strVal), n)}
			}
		}
		if maxLen, ok := schema["maxLength"]; ok {
			n := toFloat64(maxLen)
			if float64(len(strVal)) > n {
				return &ValidationError{Field: path, Message: fmt.Sprintf("length %d exceeds maxLength %g", len(strVal), n)}
			}
		}
		if patVal, ok := schema["pattern"]; ok {
			pat, _ := patVal.(string)
			re, err := j.compiledPattern(pat)
			if err != nil {
				return &ValidationError{Field: path, Message: fmt.Sprintf("invalid pattern %q: %s", pat, err)}
			}
			if !re.MatchString(strVal) {
				return &ValidationError{Field: path, Message: fmt.Sprintf("value %q does not match pattern %q", strVal, pat)}
			}
		}
	}

	// Numeric keywords (JSON numbers decode as float64)
	if numVal, ok := toNumber(v); ok {
		if minimum, ok := schema["minimum"]; ok {
			n := toFloat64(minimum)
			if numVal < n {
				return &ValidationError{Field: path, Message: fmt.Sprintf("value %g is less than minimum %g", numVal, n)}
			}
		}
		if maximum, ok := schema["maximum"]; ok {
			n := toFloat64(maximum)
			if numVal > n {
				return &ValidationError{Field: path, Message: fmt.Sprintf("value %g exceeds maximum %g", numVal, n)}
			}
		}
	}

	return nil
}

func checkJSONType(v interface{}, typStr, path string) error {
	var actualType string
	switch v.(type) {
	case map[string]interface{}:
		actualType = "object"
	case []interface{}:
		actualType = "array"
	case string:
		actualType = "string"
	case bool:
		actualType = "boolean"
	case float64:
		actualType = "number"
	case nil:
		actualType = "null"
	default:
		actualType = "unknown"
	}
	// integer is a subset of number in JSON Schema
	if typStr == "integer" {
		n, ok := v.(float64)
		if !ok || n != float64(int64(n)) {
			return &ValidationError{Field: path, Message: fmt.Sprintf("expected type integer, got %s", actualType)}
		}
		return nil
	}
	if actualType != typStr {
		return &ValidationError{Field: path, Message: fmt.Sprintf("expected type %q, got %q", typStr, actualType)}
	}
	return nil
}

func toFloat64(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func toNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}
