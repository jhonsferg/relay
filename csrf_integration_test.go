package relay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCookieJarPersistenceAcrossRequests verifies that cookies returned in
// one request are automatically included in subsequent requests (BUG-001 fix).
func TestCookieJarPersistenceAcrossRequests(t *testing.T) {
	var getRequestCount, postRequestCount int
	var postRequestHasCookie bool
	var postCookieValue string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			getRequestCount++
			http.SetCookie(w, &http.Cookie{
				Name:  "JSESSIONID",
				Value: "abc123xyz789",
				Path:  "/",
			})
			w.Header().Set("X-CSRF-Token", "token-12345")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("metadata"))
		case "POST":
			postRequestCount++
			cookie, err := r.Cookie("JSESSIONID")
			if err == nil {
				postRequestHasCookie = true
				postCookieValue = cookie.Value
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success": true}`))
		}
	}))
	defer server.Close()

	client := New(WithBaseURL(server.URL))

	getReq := client.Get("/$metadata")
	getResp, err := client.Execute(getReq)
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer PutResponse(getResp)

	if getRequestCount != 1 {
		t.Errorf("Expected 1 GET request, got %d", getRequestCount)
	}

	token := getResp.Headers.Get("X-CSRF-Token")
	if token != "token-12345" {
		t.Errorf("Expected X-CSRF-Token to be 'token-12345', got '%s'", token)
	}

	postReq := client.Post("/entity")
	data := map[string]string{"test": "data"}
	body, _ := json.Marshal(data)
	postReq = postReq.WithBody(body)
	postResp, err := client.Execute(postReq)
	if err != nil {
		t.Fatalf("POST request failed: %v", err)
	}
	defer PutResponse(postResp)

	if postRequestCount != 1 {
		t.Errorf("Expected 1 POST request, got %d", postRequestCount)
	}

	if !postRequestHasCookie {
		t.Error("Cookie from GET response was not included in POST request")
	} else if postCookieValue != "abc123xyz789" {
		t.Errorf("Expected cookie value 'abc123xyz789', got '%s'", postCookieValue)
	}
}

// TestDefaultConfigInitializesCookieJar verifies that defaultConfig()
// initialises a CookieJar by default (BUG-001 fix).
func TestDefaultConfigInitializesCookieJar(t *testing.T) {
	cfg := defaultConfig()
	if cfg.CookieJar == nil {
		t.Error("Expected defaultConfig() to initialise CookieJar, but it was nil")
	}
}
