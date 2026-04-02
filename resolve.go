package relay

import (
	"net/url"
)

// ResolutionResult holds the result of URL resolution testing.
// It provides information about how the base URL and path were combined,
// which is useful for debugging URL resolution issues.
type ResolutionResult struct {
	// URL is the final resolved URL as a string
	URL string

	// ParsedURL is the resolved URL parsed into a *url.URL structure
	ParsedURL *url.URL

	// Strategy indicates which normalisation strategy was used:
	// - "Auto" for automatic detection
	// - "RFC3986" for RFC 3986 resolution
	// - "API" for safe string normalisation
	Strategy string

	// IsAPI indicates whether the base URL was detected as an API endpoint
	IsAPI bool
}

// ResolveTest provides a way to test URL resolution without making an HTTP request.
// It takes a base URL, a relative path, and a config, then returns the resolved URL
// and information about how it was resolved. This is useful for debugging URL
// resolution behaviour and understanding which normalisation strategy is used.
//
// Example:
//
//	config := relay.New().Config()
//	result := relay.ResolveTest("http://api.example.com/v1", "Products", config)
//	fmt.Println(result.URL)       // "http://api.example.com/v1/Products"
//	fmt.Println(result.Strategy)  // "Auto" or "RFC3986" or "API"
//	fmt.Println(result.IsAPI)     // true (detected API pattern)
func ResolveTest(baseURL string, relativePath string, config *Config) *ResolutionResult {
	// Parse base URL if not already parsed
	var parsedBaseURL *url.URL
	if config.parsedBaseURL != nil {
		parsedBaseURL = config.parsedBaseURL
	} else if baseURL != "" {
		var err error
		parsedBaseURL, err = url.Parse(baseURL)
		if err != nil {
			parsedBaseURL = nil
		}
	}

	// Determine if this is an API base URL
	isAPIBase := isAPIBase(baseURL)

	// Determine the strategy that would be used
	var strategyUsed string
	useRFC3986 := false

	switch config.URLNormalisationMode {
	case NormalisationAuto:
		// Auto: use RFC 3986 for host-only, safe string for APIs
		useRFC3986 = parsedBaseURL != nil && !isAPIBase
		strategyUsed = "Auto"
	case NormalisationRFC3986:
		// Force RFC 3986
		useRFC3986 = parsedBaseURL != nil
		strategyUsed = "RFC3986"
	case NormalisationAPI:
		// Force safe string normalisation
		useRFC3986 = false
		strategyUsed = "API"
	default:
		strategyUsed = "Unknown"
	}

	var resolvedURL string

	if baseURL == "" {
		// No base URL, return path as-is
		resolvedURL = relativePath
	} else if useRFC3986 && parsedBaseURL != nil {
		// Use RFC 3986 resolution (zero-alloc, via url.ResolveReference)
		resolved := parsedBaseURL.ResolveReference(&url.URL{Path: relativePath})
		resolvedURL = resolved.String()
	} else {
		// Use safe string normalisation (preserves base path)
		// Ensure base URL ends with / and relative path doesn't start with /
		if !endsWith(baseURL, "/") {
			baseURL += "/"
		}
		// Remove leading slash from relative path to avoid double slashes
		if len(relativePath) > 0 && relativePath[0] == '/' {
			relativePath = relativePath[1:]
		}
		resolvedURL = baseURL + relativePath
	}

	// Parse the final resolved URL
	var parsedResult *url.URL
	if resolvedURL != "" {
		parsedResult, _ = url.Parse(resolvedURL)
	}

	return &ResolutionResult{
		URL:       resolvedURL,
		ParsedURL: parsedResult,
		Strategy:  strategyUsed,
		IsAPI:     isAPIBase,
	}
}

// Helper function to check if a string ends with a suffix
// (avoids importing strings package if not already used elsewhere)
func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
