// Example: Building REST APIs with Relay
//
// This example demonstrates how to use Relay to build robust HTTP clients
// for REST APIs with proper URL handling, error handling, and type safety.
//
// Key concepts:
// - URL normalization (Phase 1-3)
// - PathBuilder for clean path construction (Phase 4)
// - Configuration modes for different API patterns
// - Request/response handling best practices
// - ResolveTest for debugging URL resolution

package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	relay "github.com/jhonsferg/relay"
)

// User represents a user from the API
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Post represents a blog post from the API
type Post struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

// APIClient wraps Relay for type-safe API access
type APIClient struct {
	client  *relay.Client
	baseURL string
}

// NewAPIClient creates a new API client pointing to an example API
func NewAPIClient(baseURL string) *APIClient {
	// Phase 3: Auto-normalization adds trailing slash if needed
	// Phase 1: Smart detection chooses best URL resolution strategy
	client := relay.New(
		relay.WithBaseURL(baseURL),
		relay.WithTimeout(15*time.Second),
	)

	return &APIClient{
		client:  client,
		baseURL: baseURL,
	}
}

// GetUser retrieves a user by ID using PathBuilder for clean path construction
func (c *APIClient) GetUser(userID int) (*User, error) {
	// Phase 4: PathBuilder fluent interface for path construction
	path := relay.NewPathBuilder("/users").
		Add(fmt.Sprintf("%d", userID)).
		String()

	var user User
	resp, err := c.client.ExecuteJSON(c.client.Get(path), &user)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("server returned error: %s", resp.Status)
	}

	return &user, nil
}

// GetUserPosts retrieves all posts for a specific user
func (c *APIClient) GetUserPosts(userID int) ([]Post, error) {
	// Chain PathBuilder for nested resource paths
	path := relay.NewPathBuilder("/users").
		Add(fmt.Sprintf("%d", userID)).
		Add("posts").
		String()

	var posts []Post
	resp, err := c.client.ExecuteJSON(c.client.Get(path), &posts)
	if err != nil {
		return nil, fmt.Errorf("failed to get posts: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("server returned error: %s", resp.Status)
	}

	return posts, nil
}

// CreateUser creates a new user
func (c *APIClient) CreateUser(user *User) (*User, error) {
	path := relay.NewPathBuilder("/users").String()

	var created User
	resp, err := c.client.ExecuteJSON(
		c.client.Post(path).WithJSON(user),
		&created,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("server returned error: %s", resp.Status)
	}

	return &created, nil
}

// DebugURLResolution demonstrates URL resolution behavior (Phase 1-3)
// This shows how different base URLs are handled with PathBuilder
func (c *APIClient) DebugURLResolution(relativePath string) {
	// In real scenarios, you can use ResolveTest from the relay package to debug
	// URL resolution without making HTTP requests. This requires a Config pointer,
	// which is typically created during client initialization.
	//
	// Here we demonstrate the concept:
	// - Phase 1 automatically detects API patterns
	// - Phase 3 auto-normalization adds trailing slashes
	// - PathBuilder ensures clean path construction

	path := relay.NewPathBuilder(relativePath).String()

	fmt.Printf("URL Resolution Demo:\n")
	fmt.Printf("  Base URL:      %s\n", c.baseURL)
	fmt.Printf("  Relative path: %s\n", relativePath)
	fmt.Printf("  Built path:    %s\n", path)
	fmt.Printf("\nWith Relay client:\n")
	fmt.Printf("  Full request would go to: %s%s\n", c.baseURL, path)
	fmt.Println()
}

// Example usage patterns
func main() {
	demoMode := flag.String("demo", "local", "demo mode: local, live, or debug")
	flag.Parse()

	switch *demoMode {
	case "local":
		localAPIDemo()
	case "live":
		liveAPIDemo()
	case "debug":
		debugResolutionDemo()
	default:
		log.Fatalf("unknown demo mode: %s", *demoMode)
	}
}

// localAPIDemo demonstrates usage with a local API (JSONPlaceholder)
func localAPIDemo() {
	fmt.Println("=== Relay API Client Example ===\n")

	// Create client with JSONPlaceholder (free online API)
	client := NewAPIClient("https://jsonplaceholder.typicode.com")

	// Example 1: Get a single user
	fmt.Println("1. Fetching user #1...")
	user, err := client.GetUser(1)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("   ID: %d, Name: %s, Email: %s\n\n", user.ID, user.Name, user.Email)
	}

	// Example 2: Get posts for that user
	fmt.Println("2. Fetching posts for user #1...")
	posts, err := client.GetUserPosts(1)
	if err != nil {
		log.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("   Found %d posts:\n", len(posts))
		for i, post := range posts {
			if i < 2 { // Show first 2
				fmt.Printf("   - %s\n", post.Title)
			}
		}
		if len(posts) > 2 {
			fmt.Printf("   ... and %d more\n", len(posts)-2)
		}
		fmt.Println()
	}

	// Example 3: PathBuilder usage
	fmt.Println("3. PathBuilder examples:")
	paths := []string{
		relay.NewPathBuilder("/api/v1").Add("users").String(),
		relay.NewPathBuilder("/api/v1").Add("users").Add("123").String(),
		relay.NewPathBuilder("/api/v1").Add("users").Add("123").Add("posts").String(),
		relay.NewPathBuilder("/api/v1").Add("search").String(),
	}
	for _, path := range paths {
		fmt.Printf("   %s\n", path)
	}
	fmt.Println()

	// Example 4: Configuration modes
	fmt.Println("4. Configuration modes:")
	fmt.Println("   - NormalizationAuto (default): Smart detection of API vs host-only")
	fmt.Println("   - NormalizationRFC3986: Force RFC 3986 (zero-alloc, breaks APIs)")
	fmt.Println("   - NormalizationAPI: Force safe normalization (always preserves paths)")
	fmt.Println()
}

// liveAPIDemo demonstrates with actual API calls (optional)
func liveAPIDemo() {
	fmt.Println("=== Live API Demo ===\n")

	client := NewAPIClient("https://jsonplaceholder.typicode.com")

	// Get and display a user
	user, err := client.GetUser(2)
	if err != nil {
		log.Fatalf("Failed to get user: %v", err)
	}

	fmt.Printf("User: %s (%s)\n\n", user.Name, user.Email)

	// Get their posts
	posts, err := client.GetUserPosts(2)
	if err != nil {
		log.Fatalf("Failed to get posts: %v", err)
	}

	fmt.Printf("Posts by %s:\n", user.Name)
	for i, post := range posts {
		fmt.Printf("%d. %s\n", i+1, post.Title)
	}
}

// debugResolutionDemo shows ResolveTest utility (Phase 4)
func debugResolutionDemo() {
	fmt.Println("=== URL Resolution Debug Demo ===\n")

	// Test various API patterns
	client := NewAPIClient("https://api.example.com/v1/service")

	testCases := []string{
		"/users",
		"/users/123",
		"/users/123/posts",
		"/search",
		"products",
		"products/123",
	}

	for _, path := range testCases {
		client.DebugURLResolution(path)
	}

	fmt.Println("\n=== URL Resolution with Different Base URLs ===\n")

	// Test with host-only URL
	hostOnlyClient := NewAPIClient("https://api.example.com")
	fmt.Println("Host-only base URL (https://api.example.com):")
	hostOnlyClient.DebugURLResolution("/users/123")

	// Test with nested path
	nestedClient := NewAPIClient("https://api.example.com/v1/data")
	fmt.Println("Nested base URL (https://api.example.com/v1/data):")
	nestedClient.DebugURLResolution("/users/123")
}

// Best practices demonstrated in this example:
//
// 1. Type Safety: Wrapping relay.Client in APIClient type
//    - Provides type-safe methods for your specific API
//    - Enables IDE autocompletion
//    - Easier to test and maintain
//
// 2. Path Construction: Using PathBuilder (Phase 4)
//    - Fluent interface is readable and maintainable
//    - Automatic slash normalization
//    - Safe for building complex paths
//
// 3. Context Usage: All methods accept context.Context
//    - Enables timeout, cancellation, and tracing
//    - Follows Go best practices
//
// 4. Error Handling: Wrapping errors with context
//    - Clear error messages for debugging
//    - Preserves the error chain
//
// 5. URL Normalization: Leveraging Phase 1-3 automatically
//    - No manual slash management needed
//    - Works with both API and host-only URLs
//    - Automatic detection of the best strategy
//
// 6. Debugging: Using ResolveTest (Phase 4)
//    - Verify URL resolution without HTTP calls
//    - Useful for integration tests
//    - Helps understand the resolution strategy being used
