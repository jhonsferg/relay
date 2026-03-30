package oauth_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	relayoauth "github.com/jhonsferg/relay/ext/oauth"
	"github.com/jhonsferg/relay/testutil"
)

// tokenResponse builds a JSON token response body.
func tokenResponse(accessToken string, expiresIn int) string {
	b, _ := json.Marshal(map[string]interface{}{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
	})
	return string(b)
}

func TestOAuth2_TokenInjectedAsBearer(t *testing.T) {
	t.Parallel()

	tokenSrv := testutil.NewMockServer()
	defer tokenSrv.Close()

	apiSrv := testutil.NewMockServer()
	defer apiSrv.Close()

	tokenSrv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    tokenResponse("tok-abc", 3600),
	})
	apiSrv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "secured"})

	c := relay.New(
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relayoauth.WithClientCredentials(relayoauth.Config{
			TokenURL:     tokenSrv.URL() + "/token",
			ClientID:     "client1",
			ClientSecret: "secret1",
		}),
	)

	resp, err := c.Execute(c.Get(apiSrv.URL() + "/api"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	apiReq, err := apiSrv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	auth := apiReq.Headers.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Errorf("expected Bearer token in Authorization header, got %q", auth)
	}
	if !strings.Contains(auth, "tok-abc") {
		t.Errorf("expected 'tok-abc' in Authorization header, got %q", auth)
	}
}

func TestOAuth2_TokenCachedAcrossRequests(t *testing.T) {
	t.Parallel()

	tokenSrv := testutil.NewMockServer()
	defer tokenSrv.Close()

	apiSrv := testutil.NewMockServer()
	defer apiSrv.Close()

	tokenSrv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    tokenResponse("tok-cached", 3600),
	})
	apiSrv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	apiSrv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := relay.New(
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relayoauth.WithClientCredentials(relayoauth.Config{
			TokenURL:     tokenSrv.URL() + "/token",
			ClientID:     "c",
			ClientSecret: "s",
		}),
	)

	for i := 0; i < 2; i++ {
		_, err := c.Execute(c.Get(apiSrv.URL() + "/api"))
		if err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}

	if tokenSrv.RequestCount() != 1 {
		t.Errorf("token endpoint should be called once (caching); called %d times", tokenSrv.RequestCount())
	}
}

func TestOAuth2_AutoRefreshWhenNearExpiry(t *testing.T) {
	t.Parallel()

	tokenSrv := testutil.NewMockServer()
	defer tokenSrv.Close()

	apiSrv := testutil.NewMockServer()
	defer apiSrv.Close()

	tokenSrv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    tokenResponse("tok-first", 1),
	})
	tokenSrv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    tokenResponse("tok-second", 3600),
	})
	apiSrv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	apiSrv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := relay.New(
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relayoauth.WithClientCredentials(relayoauth.Config{
			TokenURL:     tokenSrv.URL() + "/token",
			ClientID:     "c",
			ClientSecret: "s",
			ExpiryDelta:  5 * time.Second,
		}),
	)

	_, err := c.Execute(c.Get(apiSrv.URL() + "/api"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	_, err = c.Execute(c.Get(apiSrv.URL() + "/api"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if tokenSrv.RequestCount() != 2 {
		t.Errorf("expected 2 token fetches (refresh), got %d", tokenSrv.RequestCount())
	}

	apiSrv.TakeRequest(time.Second) //nolint:errcheck
	req2, err := apiSrv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("second API request: %v", err)
	}
	if !strings.Contains(req2.Headers.Get("Authorization"), "tok-second") {
		t.Errorf("expected 'tok-second' in second request Authorization, got %q",
			req2.Headers.Get("Authorization"))
	}
}

func TestOAuth2_ErrorFromTokenEndpoint(t *testing.T) {
	t.Parallel()

	tokenSrv := testutil.NewMockServer()
	defer tokenSrv.Close()

	apiSrv := testutil.NewMockServer()
	defer apiSrv.Close()

	tokenSrv.Enqueue(testutil.MockResponse{
		Status: http.StatusInternalServerError,
		Body:   "internal error",
	})

	c := relay.New(
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relayoauth.WithClientCredentials(relayoauth.Config{
			TokenURL:     tokenSrv.URL() + "/token",
			ClientID:     "c",
			ClientSecret: "s",
		}),
	)

	_, err := c.Execute(c.Get(apiSrv.URL() + "/api"))
	if err == nil {
		t.Fatal("expected error when token endpoint returns 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500, got %q", err.Error())
	}
}

func TestOAuth2_TokenRequestIncludesScopes(t *testing.T) {
	t.Parallel()

	tokenSrv := testutil.NewMockServer()
	defer tokenSrv.Close()

	apiSrv := testutil.NewMockServer()
	defer apiSrv.Close()

	tokenSrv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    tokenResponse("tok-scoped", 3600),
	})
	apiSrv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := relay.New(
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relayoauth.WithClientCredentials(relayoauth.Config{
			TokenURL:     tokenSrv.URL() + "/token",
			ClientID:     "c",
			ClientSecret: "s",
			Scopes:       []string{"read", "write"},
		}),
	)

	_, err := c.Execute(c.Get(apiSrv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	tokenReq, err := tokenSrv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}

	body := string(tokenReq.Body)
	if !strings.Contains(body, "scope=") {
		t.Errorf("token request should include scope parameter, body: %q", body)
	}
	if !strings.Contains(body, "read") {
		t.Errorf("expected 'read' scope in token request, body: %q", body)
	}
	_ = fmt.Sprintf("token body: %s", body)
}
