package relay

import (
	"strings"
)

// PathBuilder provides a fluent interface for constructing relative paths
// by composing path segments. It uses the builder pattern to allow clean,
// chainable path construction.
//
// Example:
//
//	builder := NewPathBuilder("/api/v1")
//	path := builder.Add("users").Add("123").Add("posts").String()
//	// Result: "/api/v1/users/123/posts"
type PathBuilder struct {
	segments []string
}

// NewPathBuilder creates a new PathBuilder initialized with an optional
// base path. If base is provided, it becomes the first segment.
// If base is empty, the builder starts empty.
//
// Example:
//
//	builder := NewPathBuilder("/api/v1")  // Start with base path
//	builder := NewPathBuilder("")         // Start empty
func NewPathBuilder(base string) *PathBuilder {
	pb := &PathBuilder{
		segments: make([]string, 0, 8),
	}

	// Add base path if provided and not empty
	if base != "" {
		// Trim leading/trailing slashes from base for consistent handling
		// Use manual trimming to avoid allocation if not needed
		start := 0
		end := len(base)
		for start < end && base[start] == '/' {
			start++
		}
		for end > start && base[end-1] == '/' {
			end--
		}
		if start < end {
			pb.segments = append(pb.segments, base[start:end])
		}
	}

	return pb
}

// Add appends a new path segment to the builder. The segment is added
// as-is (slashes are handled automatically). Returns the builder for
// method chaining.
//
// Example:
//
//	builder.Add("users").Add("123").Add("profile")
func (pb *PathBuilder) Add(segment string) *PathBuilder {
	if segment != "" {
		// Remove leading/trailing slashes from segment for consistency
		// Use manual trimming to avoid allocation if not needed
		start := 0
		end := len(segment)
		for start < end && segment[start] == '/' {
			start++
		}
		for end > start && segment[end-1] == '/' {
			end--
		}
		if start < end {
			pb.segments = append(pb.segments, segment[start:end])
		}
	}
	return pb
}

// AddIfNotEmpty appends segment only if the provided condition is true.
// Useful for conditionally building paths based on configuration.
//
// Example:
//
//	builder.Add("users").AddIfNotEmpty(id != "", id).Add("posts")
func (pb *PathBuilder) AddIfNotEmpty(condition bool, segment string) *PathBuilder {
	if condition {
		return pb.Add(segment)
	}
	return pb
}

// String returns the final constructed path as a string.
// Segments are joined with "/" and the result always starts with "/".
//
// Example:
//
//	builder := NewPathBuilder("api").Add("v1").Add("users")
//	fmt.Println(builder.String())  // Output: "/api/v1/users"
func (pb *PathBuilder) String() string {
	if len(pb.segments) == 0 {
		return "/"
	}
	return "/" + strings.Join(pb.segments, "/")
}

// Len returns the number of segments in the path (excluding the leading slash).
func (pb *PathBuilder) Len() int {
	return len(pb.segments)
}

// Segments returns a slice of all segments in the path.
// This is useful for debugging or inspecting the built path.
func (pb *PathBuilder) Segments() []string {
	result := make([]string, len(pb.segments))
	copy(result, pb.segments)
	return result
}

// Reset clears all segments and returns the builder to an empty state.
// Useful for reusing a PathBuilder instance.
func (pb *PathBuilder) Reset() *PathBuilder {
	pb.segments = pb.segments[:0]
	return pb
}
