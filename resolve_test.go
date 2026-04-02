package relay

import (
	"testing"
)

// TestResolveTest_APIDetection tests API pattern detection
func TestResolveTest_APIDetection(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		isAPISig bool
	}{
		{
			name:     "API with v1",
			baseURL:  "http://api.example.com/v1",
			isAPISig: true,
		},
		{
			name:     "API with odata",
			baseURL:  "http://api.example.com/odata",
			isAPISig: true,
		},
		{
			name:     "Host only",
			baseURL:  "http://api.example.com",
			isAPISig: false,
		},
		{
			name:     "Empty URL",
			baseURL:  "",
			isAPISig: false,
		},
	}

	config := defaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveTest(tt.baseURL, "Products", config)
			if result.IsAPI != tt.isAPISig {
				t.Errorf("ResolveTest(%q) IsAPI = %v, want %v",
					tt.baseURL, result.IsAPI, tt.isAPISig)
			}
		})
	}
}

// TestResolveTest_AutoNormalisation tests with auto-normalisation enabled
func TestResolveTest_AutoNormalisation(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		relativePath string
		expectedURL  string
		isAPI        bool
	}{
		{
			name:         "API base, auto normalised",
			baseURL:      "http://api.example.com/v1",
			relativePath: "Products",
			expectedURL:  "http://api.example.com/v1/Products",
			isAPI:        true,
		},
		{
			name:         "Host only, auto normalised",
			baseURL:      "http://api.example.com",
			relativePath: "Products",
			expectedURL:  "http://api.example.com/Products",
			isAPI:        false,
		},
		{
			name:         "Empty base",
			baseURL:      "",
			relativePath: "Products",
			expectedURL:  "Products",
			isAPI:        false,
		},
	}

	config := defaultConfig()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveTest(tt.baseURL, tt.relativePath, config)
			if result.URL != tt.expectedURL {
				t.Errorf("ResolveTest(%q, %q) URL = %q, want %q",
					tt.baseURL, tt.relativePath, result.URL, tt.expectedURL)
			}
			if result.IsAPI != tt.isAPI {
				t.Errorf("ResolveTest(%q) IsAPI = %v, want %v",
					tt.baseURL, result.IsAPI, tt.isAPI)
			}
		})
	}
}

// TestResolveTest_RFC3986Mode forces RFC 3986 strategy
func TestResolveTest_RFC3986Mode(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		relativePath string
		expectedURL  string
	}{
		{
			name:         "Host only with RFC3986",
			baseURL:      "http://api.example.com",
			relativePath: "Products",
			expectedURL:  "http://api.example.com/Products",
		},
		{
			name:         "API base with RFC3986 (breaks path)",
			baseURL:      "http://api.example.com/v1",
			relativePath: "Products",
			// RFC 3986 treats "/" as absolute, so replaces /v1 with /Products
			expectedURL: "http://api.example.com/Products",
		},
	}

	config := defaultConfig()
	config.URLNormalisationMode = NormalisationRFC3986

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveTest(tt.baseURL, tt.relativePath, config)
			if result.URL != tt.expectedURL {
				t.Errorf("ResolveTest(%q, %q) with RFC3986 = %q, want %q",
					tt.baseURL, tt.relativePath, result.URL, tt.expectedURL)
			}
			if result.Strategy != "RFC3986" {
				t.Errorf("Strategy = %q, want %q", result.Strategy, "RFC3986")
			}
		})
	}
}

// TestResolveTest_APIMode forces safe string normalisation
func TestResolveTest_APIMode(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		relativePath string
		expectedURL  string
	}{
		{
			name:         "Host only with API mode",
			baseURL:      "http://api.example.com",
			relativePath: "Products",
			expectedURL:  "http://api.example.com/Products",
		},
		{
			name:         "API base with API mode (preserves path)",
			baseURL:      "http://api.example.com/v1",
			relativePath: "Products",
			expectedURL:  "http://api.example.com/v1/Products",
		},
	}

	config := defaultConfig()
	config.URLNormalisationMode = NormalisationAPI

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveTest(tt.baseURL, tt.relativePath, config)
			if result.URL != tt.expectedURL {
				t.Errorf("ResolveTest(%q, %q) with API mode = %q, want %q",
					tt.baseURL, tt.relativePath, result.URL, tt.expectedURL)
			}
			if result.Strategy != "API" {
				t.Errorf("Strategy = %q, want %q", result.Strategy, "API")
			}
		})
	}
}

// TestResolveTest_ParsedURL validates parsed URL output
func TestResolveTest_ParsedURL(t *testing.T) {
	config := defaultConfig()
	result := ResolveTest("http://api.example.com/v1", "Products", config)

	if result.ParsedURL == nil {
		t.Errorf("ParsedURL should not be nil")
		return
	}

	if result.ParsedURL.Host != "api.example.com" {
		t.Errorf("ParsedURL.Host = %q, want %q", result.ParsedURL.Host, "api.example.com")
	}

	if result.ParsedURL.Scheme != "http" {
		t.Errorf("ParsedURL.Scheme = %q, want %q", result.ParsedURL.Scheme, "http")
	}
}

// TestResolveTest_ComplexPaths tests with multi-segment paths
func TestResolveTest_ComplexPaths(t *testing.T) {
	config := defaultConfig()

	tests := []struct {
		name         string
		baseURL      string
		relativePath string
		expectedURL  string
	}{
		{
			name:         "deep base path",
			baseURL:      "http://api.example.com/api/v1/data",
			relativePath: "search",
			expectedURL:  "http://api.example.com/api/v1/data/search",
		},
		{
			name:         "relative path without leading slash",
			baseURL:      "http://api.example.com/v1",
			relativePath: "users/123/posts",
			expectedURL:  "http://api.example.com/v1/users/123/posts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveTest(tt.baseURL, tt.relativePath, config)
			if result.URL != tt.expectedURL {
				t.Errorf("ResolveTest(%q, %q) = %q, want %q",
					tt.baseURL, tt.relativePath, result.URL, tt.expectedURL)
			}
		})
	}
}

// TestResolveTest_Strategy verifies strategy indication
func TestResolveTest_Strategy(t *testing.T) {
	tests := []struct {
		name     string
		mode     URLNormalisationMode
		baseURL  string
		expected string
	}{
		{
			name:     "Auto mode",
			mode:     NormalisationAuto,
			baseURL:  "http://api.example.com/v1",
			expected: "Auto",
		},
		{
			name:     "RFC3986 mode",
			mode:     NormalisationRFC3986,
			baseURL:  "http://api.example.com/v1",
			expected: "RFC3986",
		},
		{
			name:     "API mode",
			mode:     NormalisationAPI,
			baseURL:  "http://api.example.com/v1",
			expected: "API",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := defaultConfig()
			config.URLNormalisationMode = tt.mode

			result := ResolveTest(tt.baseURL, "Products", config)
			if result.Strategy != tt.expected {
				t.Errorf("Strategy = %q, want %q", result.Strategy, tt.expected)
			}
		})
	}
}

// TestResolveTest_AutoModeDetection tests automatic strategy selection
func TestResolveTest_AutoModeDetection(t *testing.T) {
	config := defaultConfig()
	config.URLNormalisationMode = NormalisationAuto

	tests := []struct {
		name    string
		baseURL string
		useRFC  bool // true if should use RFC3986, false if safe string
	}{
		{
			name:    "Host only (should use RFC3986)",
			baseURL: "http://api.example.com",
			useRFC:  true,
		},
		{
			name:    "API path (should use safe string)",
			baseURL: "http://api.example.com/v1",
			useRFC:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveTest(tt.baseURL, "Products", config)
			// In Auto mode, the actual URL might be the same, but we can check
			// if it's an API base
			isAPI := result.IsAPI
			if isAPI == tt.useRFC { // API base should NOT use RFC3986
				t.Errorf("API detection: isAPI=%v, expected RFC=%v", isAPI, tt.useRFC)
			}
		})
	}
}

// TestResolveTest_WithTrailingSlash tests base URLs with trailing slashes
func TestResolveTest_WithTrailingSlash(t *testing.T) {
	config := defaultConfig()

	tests := []struct {
		name         string
		baseURL      string
		relativePath string
		expectedURL  string
	}{
		{
			name:         "base with trailing slash",
			baseURL:      "http://api.example.com/v1/",
			relativePath: "Products",
			expectedURL:  "http://api.example.com/v1/Products",
		},
		{
			name:         "relative with leading slash",
			baseURL:      "http://api.example.com/v1",
			relativePath: "/Products",
			expectedURL:  "http://api.example.com/v1/Products",
		},
		{
			name:         "both with slashes",
			baseURL:      "http://api.example.com/v1/",
			relativePath: "/Products",
			expectedURL:  "http://api.example.com/v1/Products",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveTest(tt.baseURL, tt.relativePath, config)
			if result.URL != tt.expectedURL {
				t.Errorf("ResolveTest(%q, %q) = %q, want %q",
					tt.baseURL, tt.relativePath, result.URL, tt.expectedURL)
			}
		})
	}
}
