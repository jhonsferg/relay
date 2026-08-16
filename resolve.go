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
	isAPIBase := parsedBaseURL != nil && isAPIBaseParsed(parsedBaseURL)

	// Determine the strategy name to report (purely informational - the
	// actual resolution below is delegated to resolveBaseAndPath, which
	// makes this same determination itself).
	var strategyUsed string
	switch config.URLNormalisationMode {
	case NormalisationAuto:
		strategyUsed = "Auto"
	case NormalisationRFC3986:
		strategyUsed = "RFC3986"
	case NormalisationAPI:
		strategyUsed = "API"
	default:
		strategyUsed = "Unknown"
	}

	// Delegate to the same resolution logic build() uses for real requests
	// (request.go), so this debugging helper can never silently diverge
	// from what an actual request would resolve to.
	var resolvedURL string
	if baseURL == "" {
		// No base URL, return path as-is
		resolvedURL = relativePath
	} else {
		resolvedURL = resolveBaseAndPath(baseURL, parsedBaseURL, relativePath, config.URLNormalisationMode)
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
