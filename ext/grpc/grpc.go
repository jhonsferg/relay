// Package grpc provides gRPC-Gateway integration middleware for relay HTTP
// clients. It bridges idiomatic gRPC metadata conventions into standard HTTP
// headers so that relay clients can communicate with grpc-gateway proxies
// without importing any gRPC or protobuf dependencies.
//
// # What this package does
//
// gRPC-Gateway translates HTTP/JSON requests to gRPC calls. It uses a set of
// header conventions that differ from typical REST APIs:
//
//   - Metadata is passed as headers prefixed with "Grpc-Metadata-".
//   - Outgoing binary metadata uses the "-Bin" suffix and base64 encoding.
//   - The caller's timeout is forwarded as "Grpc-Timeout".
//
// This package provides relay options that automatically apply those conventions.
//
// # Usage
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relaygrpc "github.com/jhonsferg/relay/ext/grpc"
//	)
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaygrpc.WithMetadata("x-request-id", requestID),
//	    relaygrpc.WithMetadata("x-tenant-id", tenantID),
//	)
//
// # Per-request metadata
//
// Use [SetMetadata] to attach metadata on a single request:
//
//	resp, err := client.Execute(
//	    client.Post("/v1/users").Apply(relaygrpc.SetMetadata("x-idempotency-key", key)),
//	)
//
// # Timeout forwarding
//
// Pass [WithTimeoutHeader] to forward the remaining context deadline as a
// gRPC-Timeout header:
//
//	client := relay.New(
//	    relay.WithBaseURL("https://grpc-gateway.example.com"),
//	    relaygrpc.WithTimeoutHeader(),
//	)
package grpc

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jhonsferg/relay"
)

const (
	// metadataPrefix is prepended to all outgoing metadata keys.
	metadataPrefix = "Grpc-Metadata-"
	// timeoutHeader is the gRPC-Gateway timeout header name.
	timeoutHeader = "Grpc-Timeout"
	// binSuffix marks binary metadata that must be base64-encoded.
	binSuffix = "-Bin"
)

// WithMetadata returns a [relay.Option] that attaches a gRPC metadata entry
// to every request as a "Grpc-Metadata-<key>" header. The key is
// canonicalised to title-case by net/http automatically.
//
// For binary values use [WithBinaryMetadata].
func WithMetadata(key, value string) relay.Option {
	header := metadataPrefix + key
	return relay.WithOnBeforeRequest(func(_ context.Context, req *relay.Request) error {
		req.WithHeader(header, value)
		return nil
	})
}

// WithBinaryMetadata returns a [relay.Option] that attaches a base64-encoded
// binary metadata entry to every request. The header name is suffixed with
// "-Bin" per the gRPC convention.
func WithBinaryMetadata(key string, value []byte) relay.Option {
	header := metadataPrefix + key + binSuffix
	encoded := base64.StdEncoding.EncodeToString(value)
	return relay.WithOnBeforeRequest(func(_ context.Context, req *relay.Request) error {
		req.WithHeader(header, encoded)
		return nil
	})
}

// WithTimeoutHeader returns a [relay.Option] that inspects the request's
// context deadline and, when one is present, forwards the remaining duration
// as a "Grpc-Timeout" header in gRPC timeout format (e.g. "5000m" for 5 s).
// Requests without a deadline are sent without the header.
func WithTimeoutHeader() relay.Option {
	return relay.WithOnBeforeRequest(func(ctx context.Context, req *relay.Request) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil
		}
		req.WithHeader(timeoutHeader, formatGRPCTimeout(remaining))
		return nil
	})
}

// SetMetadata returns a request modifier function that sets a single gRPC
// metadata header on one specific request. Pass the result to
// [relay.Request.WithHeader] or apply it inline during request construction:
//
//	req := client.Post("/v1/resource")
//	SetMetadata("x-request-id", id)(req)
func SetMetadata(key, value string) func(*relay.Request) *relay.Request {
	header := metadataPrefix + key
	return func(req *relay.Request) *relay.Request {
		return req.WithHeader(header, value)
	}
}

// SetBinaryMetadata returns a request modifier function that sets a single
// base64-encoded binary metadata header on one specific request.
func SetBinaryMetadata(key string, value []byte) func(*relay.Request) *relay.Request {
	header := metadataPrefix + key + binSuffix
	encoded := base64.StdEncoding.EncodeToString(value)
	return func(req *relay.Request) *relay.Request {
		return req.WithHeader(header, encoded)
	}
}

// ParseMetadata extracts gRPC metadata from an HTTP header map. It returns a
// map of lowercased metadata keys (without the "Grpc-Metadata-" prefix) to
// their values. Binary metadata (keys ending in "-bin") is base64-decoded.
//
// This is useful for inspecting metadata echoed back in responses.
func ParseMetadata(headers map[string][]string) (map[string]string, error) {
	result := make(map[string]string)
	prefix := strings.ToLower(metadataPrefix)
	for rawKey, vals := range headers {
		lower := strings.ToLower(rawKey)
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		key := lower[len(prefix):]
		if len(vals) == 0 {
			continue
		}
		val := vals[0]
		if strings.HasSuffix(key, strings.ToLower(binSuffix)) {
			decoded, err := base64.StdEncoding.DecodeString(val)
			if err != nil {
				return nil, fmt.Errorf("grpc: decode binary metadata %q: %w", key, err)
			}
			result[key] = string(decoded)
		} else {
			result[key] = val
		}
	}
	return result, nil
}

// formatGRPCTimeout converts a duration to the gRPC timeout wire format.
// gRPC timeout format: <value><unit> where unit is H, M, S, m (millis), u, n.
// We use milliseconds for sub-second durations and seconds otherwise.
func formatGRPCTimeout(d time.Duration) string {
	if d >= time.Hour {
		return fmt.Sprintf("%dH", int64(d/time.Hour))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dM", int64(d/time.Minute))
	}
	if d >= time.Second {
		return fmt.Sprintf("%dS", int64(d/time.Second))
	}
	return fmt.Sprintf("%dm", int64(d/time.Millisecond))
}
