package relay

import (
	"testing"
)

// TestNewPathBuilder tests the PathBuilder constructor
func TestNewPathBuilder(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		expected string
	}{
		{
			name:     "empty base",
			base:     "",
			expected: "/",
		},
		{
			name:     "simple base",
			base:     "api",
			expected: "/api",
		},
		{
			name:     "base with leading slash",
			base:     "/api",
			expected: "/api",
		},
		{
			name:     "base with trailing slash",
			base:     "api/",
			expected: "/api",
		},
		{
			name:     "base with both slashes",
			base:     "/api/",
			expected: "/api",
		},
		{
			name:     "multi-segment base",
			base:     "/api/v1/data",
			expected: "/api/v1/data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := NewPathBuilder(tt.base)
			result := pb.String()
			if result != tt.expected {
				t.Errorf("NewPathBuilder(%q).String() = %q, want %q", tt.base, result, tt.expected)
			}
		})
	}
}

// TestPathBuilder_Add tests adding segments to the builder
func TestPathBuilder_Add(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		adds     []string
		expected string
	}{
		{
			name:     "single segment",
			base:     "/api",
			adds:     []string{"users"},
			expected: "/api/users",
		},
		{
			name:     "multiple segments",
			base:     "/api",
			adds:     []string{"users", "123", "posts"},
			expected: "/api/users/123/posts",
		},
		{
			name:     "segment with slashes",
			base:     "/api",
			adds:     []string{"/users/", "/123/"},
			expected: "/api/users/123",
		},
		{
			name:     "empty base, segments",
			base:     "",
			adds:     []string{"users", "123"},
			expected: "/users/123",
		},
		{
			name:     "empty segment ignored",
			base:     "/api",
			adds:     []string{"users", "", "posts"},
			expected: "/api/users/posts",
		},
		{
			name:     "url safe segments",
			base:     "/api/v1",
			adds:     []string{"search", "users"},
			expected: "/api/v1/search/users",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := NewPathBuilder(tt.base)
			for _, segment := range tt.adds {
				pb.Add(segment)
			}
			result := pb.String()
			if result != tt.expected {
				t.Errorf("PathBuilder.Add() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestPathBuilder_Chaining tests method chaining
func TestPathBuilder_Chaining(t *testing.T) {
	pb := NewPathBuilder("/api").
		Add("v1").
		Add("users").
		Add("123").
		Add("posts")

	expected := "/api/v1/users/123/posts"
	result := pb.String()

	if result != expected {
		t.Errorf("Chaining: got %q, want %q", result, expected)
	}
}

// TestPathBuilder_AddIfNotEmpty tests conditional adding
func TestPathBuilder_AddIfNotEmpty(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		conditions []struct {
			condition bool
			segment   string
		}
		expected string
	}{
		{
			name: "all true conditions",
			base: "/api",
			conditions: []struct {
				condition bool
				segment   string
			}{
				{true, "users"},
				{true, "123"},
				{true, "posts"},
			},
			expected: "/api/users/123/posts",
		},
		{
			name: "mixed conditions",
			base: "/api",
			conditions: []struct {
				condition bool
				segment   string
			}{
				{true, "users"},
				{false, "123"},
				{true, "posts"},
			},
			expected: "/api/users/posts",
		},
		{
			name: "all false conditions",
			base: "/api",
			conditions: []struct {
				condition bool
				segment   string
			}{
				{false, "users"},
				{false, "123"},
			},
			expected: "/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := NewPathBuilder(tt.base)
			for _, cond := range tt.conditions {
				pb.AddIfNotEmpty(cond.condition, cond.segment)
			}
			result := pb.String()
			if result != tt.expected {
				t.Errorf("AddIfNotEmpty: got %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestPathBuilder_Len tests segment count
func TestPathBuilder_Len(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		adds     []string
		expected int
	}{
		{
			name:     "empty base",
			base:     "",
			adds:     []string{},
			expected: 0,
		},
		{
			name:     "base only",
			base:     "/api",
			adds:     []string{},
			expected: 1,
		},
		{
			name:     "base with segments",
			base:     "/api",
			adds:     []string{"v1", "users"},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := NewPathBuilder(tt.base)
			for _, segment := range tt.adds {
				pb.Add(segment)
			}
			result := pb.Len()
			if result != tt.expected {
				t.Errorf("Len() = %d, want %d", result, tt.expected)
			}
		})
	}
}

// TestPathBuilder_Segments tests segment retrieval
func TestPathBuilder_Segments(t *testing.T) {
	pb := NewPathBuilder("/api").Add("v1").Add("users")
	segments := pb.Segments()

	expected := []string{"api", "v1", "users"}
	if len(segments) != len(expected) {
		t.Errorf("Segments() length = %d, want %d", len(segments), len(expected))
		return
	}

	for i, seg := range segments {
		if seg != expected[i] {
			t.Errorf("Segments()[%d] = %q, want %q", i, seg, expected[i])
		}
	}
}

// TestPathBuilder_Reset tests resetting the builder
func TestPathBuilder_Reset(t *testing.T) {
	pb := NewPathBuilder("/api").Add("v1").Add("users")

	// Before reset
	if pb.String() != "/api/v1/users" {
		t.Errorf("Before reset: got %q, want %q", pb.String(), "/api/v1/users")
	}

	// After reset
	pb.Reset()
	if pb.String() != "/" {
		t.Errorf("After reset: got %q, want %q", pb.String(), "/")
	}

	// Can reuse after reset
	pb.Add("different").Add("path")
	if pb.String() != "/different/path" {
		t.Errorf("After reuse: got %q, want %q", pb.String(), "/different/path")
	}
}

// TestPathBuilder_SpecialCharacters tests handling of special characters
func TestPathBuilder_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		segments []string
		expected string
	}{
		{
			name:     "hyphens",
			segments: []string{"my-api", "my-resource"},
			expected: "/my-api/my-resource",
		},
		{
			name:     "underscores",
			segments: []string{"my_api", "my_resource"},
			expected: "/my_api/my_resource",
		},
		{
			name:     "dots",
			segments: []string{"v1.0", "users"},
			expected: "/v1.0/users",
		},
		{
			name:     "numbers",
			segments: []string{"v2", "123", "456"},
			expected: "/v2/123/456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb := NewPathBuilder("")
			for _, seg := range tt.segments {
				pb.Add(seg)
			}
			result := pb.String()
			if result != tt.expected {
				t.Errorf("Special chars: got %q, want %q", result, tt.expected)
			}
		})
	}
}
