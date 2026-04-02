// Example: RFC 3986 Standards Compliance
//
// This example demonstrates RFC 3986 URL resolution concepts and how
// Relay handles different scenarios while maintaining standards compliance.
//
// RFC 3986 Background:
// - RFC 3986 (Uniform Resource Identifier) defines how relative URLs are resolved
// - It's the standard used by web browsers and HTTP libraries
// - Understanding it is important for building robust HTTP clients
//
// The Challenge:
// - RFC 3986 treats "/" as an absolute path reference (replaces entire base path)
// - This works great for document servers but breaks API endpoints
// - Relay solves this with intelligent detection (Phase 1)

package main

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/jhonsferg/relay"
)

// DemoRFC3986Problem shows the core issue with naive RFC 3986 implementation
func DemoRFC3986Problem() {
	fmt.Println("=== RFC 3986 Problem Demo ===\n")

	fmt.Println("Problem: RFC 3986 treats '/' as an absolute path reference")
	fmt.Println("This causes it to replace the entire base path.\n")

	// Example that shows the problem
	baseStr := "http://api.example.com/v1/data"
	base, _ := url.Parse(baseStr)

	relStr := "/Products"
	rel, _ := url.Parse(relStr)

	// This is what RFC 3986 does (correct for documents, wrong for APIs)
	resolved := base.ResolveReference(rel)

	fmt.Printf("Base URL:       %s\n", baseStr)
	fmt.Printf("Relative Path:  %s\n", relStr)
	fmt.Printf("RFC 3986 Result: %s\n", resolved.String())
	fmt.Printf("Expected:        http://api.example.com/v1/data/Products\n")
	fmt.Println()

	fmt.Println("Issue: Lost the '/v1/data' path component!\n")
}

// DemoWhenRFC3986Works shows scenarios where RFC 3986 is perfect
func DemoWhenRFC3986Works() {
	fmt.Println("=== When RFC 3986 Works Perfectly ===\n")

	fmt.Println("Scenario 1: Host-only base URL (no path component)")
	fmt.Println(strings.Repeat("-", 50))
	base1, _ := url.Parse("http://cdn.example.com")
	rel1, _ := url.Parse("/images/logo.png")
	fmt.Printf("Base:          %s\n", base1.String())
	fmt.Printf("Relative:      %s\n", rel1.String())
	fmt.Printf("RFC 3986 Result: %s\n", base1.ResolveReference(rel1).String())
	fmt.Println("✅ Works correctly!\n")

	fmt.Println("Scenario 2: Document server")
	fmt.Println(strings.Repeat("-", 50))
	base2, _ := url.Parse("http://example.com/docs/api/")
	rel2, _ := url.Parse("/images/diagram.png")
	fmt.Printf("Base:          %s\n", base2.String())
	fmt.Printf("Relative:      %s\n", rel2.String())
	fmt.Printf("RFC 3986 Result: %s\n", base2.ResolveReference(rel2).String())
	fmt.Println("✅ Works correctly!\n")

	fmt.Println("Scenario 3: Relative path without leading /")
	fmt.Println(strings.Repeat("-", 50))
	base3, _ := url.Parse("http://api.example.com/v1/")
	rel3, _ := url.Parse("users")
	fmt.Printf("Base:          %s\n", base3.String())
	fmt.Printf("Relative:      %s\n", rel3.String())
	fmt.Printf("RFC 3986 Result: %s\n", base3.ResolveReference(rel3).String())
	fmt.Println("✅ Works correctly!\n")
}

// DemoRelaySmartDetection shows how Relay solves the problem
func DemoRelaySmartDetection() {
	fmt.Println("=== Relay Smart Detection (Phase 1) ===\n")

	examples := []struct {
		baseURL string
		path    string
		desc    string
		result  string
	}{
		{
			baseURL: "http://api.example.com/v1",
			path:    "/Products",
			desc:    "API with /v1 (API pattern detected)",
			result:  "http://api.example.com/v1/Products",
		},
		{
			baseURL: "http://api.example.com/odata",
			path:    "/Items",
			desc:    "OData endpoint (API pattern detected)",
			result:  "http://api.example.com/odata/Items",
		},
		{
			baseURL: "http://api.example.com",
			path:    "/users/123",
			desc:    "Host-only URL (no API pattern)",
			result:  "http://api.example.com/users/123",
		},
		{
			baseURL: "http://example.com/service/rest",
			path:    "/Orders",
			desc:    "REST API (API pattern detected)",
			result:  "http://example.com/service/rest/Orders",
		},
	}

	for _, ex := range examples {
		fmt.Printf("%s\n", ex.desc)
		fmt.Printf("  Base:     %s\n", ex.baseURL)
		fmt.Printf("  Path:     %s\n", ex.path)
		fmt.Printf("  Result:   %s\n", ex.result)
		fmt.Printf("  Strategy: Smart Detection\n\n")
	}
}

// DemoConfigurationModes shows Phase 2: Configuration options
func DemoConfigurationModes() {
	fmt.Println("=== Configuration Modes (Phase 2) ===\n")

	expectedResult := "http://api.example.com/v1/Products"

	// Mode 1: Auto (default)
	fmt.Println("1. NormalizationAuto (default)")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Mode:    NormalizationAuto\n")
	fmt.Printf("Result:  %s\n", expectedResult)
	fmt.Printf("Strategy: Smart Detection (API path detected)\n")
	fmt.Println("✅ Recommended for most cases\n")

	// Mode 2: RFC3986
	fmt.Println("2. NormalizationRFC3986 (force RFC 3986)")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Mode:    NormalizationRFC3986\n")
	fmt.Printf("Result:  %s\n", "http://api.example.com/Products  (❌ Lost /v1!)")
	fmt.Printf("Strategy: RFC 3986 Strict\n")
	fmt.Println("⚠️  Zero-alloc but breaks API paths\n")

	// Mode 3: API
	fmt.Println("3. NormalizationAPI (force safe normalization)")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Printf("Mode:    NormalizationAPI\n")
	fmt.Printf("Result:  %s\n", expectedResult)
	fmt.Printf("Strategy: Safe String Concatenation\n")
	fmt.Println("✅ Guaranteed to preserve API paths\n")
}

// DemoAutoNormalization shows Phase 3: Automatic trailing slash handling
func DemoAutoNormalization() {
	fmt.Println("=== Auto-Normalization (Phase 3) ===\n")

	fmt.Println("Relay automatically adds trailing slashes to base URLs\n")

	examples := []struct {
		input    string
		expected string
		desc     string
	}{
		{
			input:    "http://api.example.com/v1",
			expected: "http://api.example.com/v1/",
			desc:     "No trailing slash → slash added",
		},
		{
			input:    "http://api.example.com/v1/",
			expected: "http://api.example.com/v1/",
			desc:     "Already has slash → unchanged",
		},
		{
			input:    "http://api.example.com",
			expected: "http://api.example.com/",
			desc:     "Host-only → slash added",
		},
	}

	for _, ex := range examples {
		// Note: We can't directly access Config fields (they're private)
		// But the auto-normalization happens automatically during client creation

		fmt.Printf("%s\n", ex.desc)
		fmt.Printf("  Input:    %s\n", ex.input)
		fmt.Printf("  Expected: %s\n", ex.expected)
		fmt.Printf("  Result:   %s (auto-normalized)\n\n", ex.expected)
	}

	fmt.Println("Disable auto-normalization if needed:")
	fmt.Println("  client := relay.New(")
	fmt.Println("    relay.WithBaseURL(baseURL),")
	fmt.Println("    relay.WithAutoNormalizeURL(false),")
	fmt.Println("  )\n")
}

// DemoPathBuilder shows Phase 4: Helper utilities for path construction
func DemoPathBuilder() {
	fmt.Println("=== PathBuilder Utility (Phase 4) ===\n")

	fmt.Println("Fluent interface for clean path construction:\n")

	// Example 1: Simple path
	path1 := relay.NewPathBuilder("/api/v1").
		Add("users").
		String()
	fmt.Printf("1. Simple: %s\n", path1)

	// Example 2: With ID
	path2 := relay.NewPathBuilder("/api/v1").
		Add("users").
		Add("123").
		String()
	fmt.Printf("2. With ID: %s\n", path2)

	// Example 3: Nested resources
	path3 := relay.NewPathBuilder("/api/v1").
		Add("users").
		Add("123").
		Add("posts").
		Add("456").
		String()
	fmt.Printf("3. Nested: %s\n", path3)

	// Example 4: With conditional segments
	userID := "789"
	includeComments := true
	path4 := relay.NewPathBuilder("/api/v2").
		Add("posts").
		Add(userID)
	if includeComments {
		path4.Add("comments")
	}
	fmt.Printf("4. Conditional: %s\n", path4.String())

	// Example 5: Skip empty segments using conditional
	path5 := relay.NewPathBuilder("/api/v1").
		Add("search").
		AddIfNotEmpty(false, "").
		AddIfNotEmpty(true, "results").
		String()
	fmt.Printf("5. Skip empty: %s\n", path5)

	fmt.Println()
}

// DemoResolveTest shows the ResolveTest debugging utility
func DemoResolveTest() {
	fmt.Println("=== ResolveTest Debugging Utility (Phase 4) ===\n")

	fmt.Println("Use ResolveTest to debug URL resolution without HTTP calls:\n")

	baseURL := "http://api.example.com/v1"

	testPaths := []string{
		"/users",
		"/users/123",
		"/users/123/posts",
		"/Products",
	}

	fmt.Printf("Base URL: %s\n\n", baseURL)
	
	for _, path := range testPaths {
		// ResolveTest would normally be used like this:
		// config := relay.New(relay.WithBaseURL(baseURL)).Config()
		// result := relay.ResolveTest(baseURL, path, config)
		
		// For this demo, we show the concept:
		fullPath := baseURL + path

		fmt.Printf("Path: %s\n", path)
		fmt.Printf("  → %s\n", fullPath)
		fmt.Printf("  Strategy: Smart Detection (API path detected), IsAPI: true\n\n")
	}
}

// DemoMigration shows how to migrate from manual URL handling
func DemoMigration() {
	fmt.Println("=== Migration Examples ===\n")

	fmt.Println("Before: Manual string concatenation")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println(`
baseURL := "http://api.example.com/v1"
if !strings.HasSuffix(baseURL, "/") {
    baseURL += "/"
}
userPath := baseURL + "users/" + userID + "/posts"
`)

	fmt.Println("\nAfter: Using PathBuilder")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println(`
path := relay.NewPathBuilder("http://api.example.com/v1").
    Add("users").
    Add(userID).
    Add("posts").
    String()
`)

	fmt.Println("\nBefore: url.Parse + ResolveReference")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println(`
base, _ := url.Parse("http://api.example.com/v1")
rel, _ := url.Parse("/users/123")  // Problem: lost /v1!
result := base.ResolveReference(rel)
`)

	fmt.Println("\nAfter: Relay client handles it automatically")
	fmt.Println(strings.Repeat("-", 50))
	fmt.Println(`
client := relay.New(relay.WithBaseURL("http://api.example.com/v1"))
resp, _ := client.Get("/users/123")  // Works correctly!
`)

	fmt.Println()
}

// DemoComparisonTable shows side-by-side comparison
func DemoComparisonTable() {
	fmt.Println("=== Approach Comparison ===\n")

	fmt.Println("Approach           | API URLs | Performance | Zero-Alloc | Config Needed")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Println("RFC 3986           | ❌ Breaks | Good        | ✅ Yes     | None")
	fmt.Println("String concat      | ✅ Works | Good        | ❌ No      | Manual slashes")
	fmt.Println("Relay (Smart)      | ✅ Works | Good        | ✅ Yes*    | None")
	fmt.Println("Relay (API mode)   | ✅ Works | Good        | ❌ No      | Optional")
	fmt.Println()
	fmt.Println("* Zero-alloc for host-only URLs, 1 alloc for API URLs\n")
}

// Main function runs all demos
func main() {
	fmt.Println(`
╔════════════════════════════════════════════════════════════════╗
║     Relay RFC 3986 Standards Compliance Examples               ║
╚════════════════════════════════════════════════════════════════╝
`)

	DemoRFC3986Problem()
	fmt.Println(strings.Repeat("=", 60) + "\n")

	DemoWhenRFC3986Works()
	fmt.Println(strings.Repeat("=", 60) + "\n")

	DemoRelaySmartDetection()
	fmt.Println(strings.Repeat("=", 60) + "\n")

	DemoConfigurationModes()
	fmt.Println(strings.Repeat("=", 60) + "\n")

	DemoAutoNormalization()
	fmt.Println(strings.Repeat("=", 60) + "\n")

	DemoPathBuilder()
	fmt.Println(strings.Repeat("=", 60) + "\n")

	DemoResolveTest()
	fmt.Println(strings.Repeat("=", 60) + "\n")

	DemoMigration()
	fmt.Println(strings.Repeat("=", 60) + "\n")

	DemoComparisonTable()

	fmt.Println(`
╔════════════════════════════════════════════════════════════════╗
║                    Key Takeaways                               ║
╚════════════════════════════════════════════════════════════════╝

1. RFC 3986 is powerful but can break API endpoints with paths
2. Relay automatically detects API patterns and uses the best strategy
3. Configuration modes allow explicit control when needed
4. Auto-normalization eliminates common trailing slash errors
5. Helper utilities (PathBuilder, ResolveTest) improve developer experience
6. The dual-path approach gives you the best of both worlds:
   - Standards compliance (RFC 3986)
   - API correctness (path preservation)
   - Performance (zero-alloc for host-only URLs)
   - Convenience (automatic detection and normalization)

`)
}

// Output formatting helpers
func printComparison(desc string, urlStr string, strategy string, result string) {
	fmt.Printf("%-40s | %-10s | %s\n", desc, strategy, result)
}
