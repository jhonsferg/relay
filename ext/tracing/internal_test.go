package tracing

import (
	"net/url"
	"testing"
)

// TestRedactURL guards against credential leakage in the url.full span
// attribute, mirroring the same fix already applied to ext/otel (semconv
// v1.26 requires url.full to never contain userinfo).
func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no credentials",
			in:   "https://example.com/path?q=1",
			want: "https://example.com/path?q=1",
		},
		{
			name: "userinfo stripped",
			in:   "https://user:secret@example.com/path",
			want: "https://example.com/path",
		},
		{
			name: "username only stripped",
			in:   "https://user@example.com/path",
			want: "https://example.com/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.in)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", tt.in, err)
			}
			if got := redactURL(u); got != tt.want {
				t.Errorf("redactURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
