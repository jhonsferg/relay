// Package sigv4 signs relay HTTP requests with AWS Signature Version 4 (SigV4)
// using github.com/aws/aws-sdk-go-v2/aws/signer/v4. This enables relay clients
// to call any AWS service that uses SigV4 authentication: API Gateway, S3,
// Lambda Function URLs, AppSync, etc.
//
// Usage:
//
//	import (
//	    "github.com/aws/aws-sdk-go-v2/config"
//	    "github.com/aws/aws-sdk-go-v2/credentials"
//	    "github.com/jhonsferg/relay"
//	    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
//	)
//
//	// Option A: static credentials (dev / testing)
//	creds := credentials.NewStaticCredentialsProvider("AKID", "SECRET_KEY", "")
//	client := relay.New(
//	    relay.WithBaseURL("https://execute-api.us-east-1.amazonaws.com"),
//	    relaysigv4.WithSigV4(creds, "execute-api", "us-east-1"),
//	)
//
//	// Option B: credentials from the default AWS credential chain
//	cfg, _ := config.LoadDefaultConfig(context.Background())
//	client := relay.New(
//	    relay.WithBaseURL("https://s3.amazonaws.com"),
//	    relaysigv4.WithSigV4(cfg.Credentials, "s3", "us-east-1"),
//	)
//
// # Payload hashing
//
// By default the request body is read, SHA-256 hashed, and restored before
// signing - this is required by services such as S3. For services that accept
// unsigned payloads (e.g. API Gateway over HTTPS) you can skip body hashing
// with [WithUnsignedPayload]:
//
//	relaysigv4.WithSigV4(creds, "execute-api", "us-east-1",
//	    relaysigv4.WithUnsignedPayload())
//
// # Headers added
//
// The signer injects: Authorization, X-Amz-Date, and (when a session token is
// present) X-Amz-Security-Token. Existing values for these headers are
// overwritten.
package sigv4

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"github.com/jhonsferg/relay"
)

// emptyPayloadHash is the SHA-256 digest of an empty byte slice.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// unsignedPayloadValue is the sentinel used by services that accept unsigned payloads.
const unsignedPayloadValue = "UNSIGNED-PAYLOAD"

// Option configures the sigv4 transport.
type Option func(*sigv4Config)

type sigv4Config struct {
	unsignedPayload bool
}

// WithUnsignedPayload skips body hashing and uses the "UNSIGNED-PAYLOAD"
// sentinel value. Use this for services that do not require payload integrity
// checks (e.g. API Gateway, Lambda Function URLs over HTTPS).
func WithUnsignedPayload() Option {
	return func(c *sigv4Config) { c.unsignedPayload = true }
}

// WithSigV4 returns a [relay.Option] that signs every outgoing request with
// AWS Signature Version 4. provider supplies AWS credentials; service and
// region are the AWS service name and region (e.g. "s3", "us-east-1").
func WithSigV4(provider aws.CredentialsProvider, service, region string, opts ...Option) relay.Option {
	cfg := sigv4Config{}
	for _, o := range opts {
		o(&cfg)
	}
	signer := v4.NewSigner()
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &sigv4Transport{
			base:     next,
			provider: provider,
			signer:   signer,
			service:  service,
			region:   region,
			cfg:      cfg,
		}
	})
}

type sigv4Transport struct {
	base     http.RoundTripper
	provider aws.CredentialsProvider
	signer   *v4.Signer
	service  string
	region   string
	cfg      sigv4Config
}

func (t *sigv4Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request so we do not mutate the caller's headers.
	req = req.Clone(req.Context())

	ctx := req.Context()

	creds, err := t.provider.Retrieve(ctx)
	if err != nil {
		return nil, fmt.Errorf("sigv4: retrieve credentials: %w", err)
	}

	payloadHash, err := t.hashBody(req)
	if err != nil {
		return nil, fmt.Errorf("sigv4: hash body: %w", err)
	}

	if err := t.signer.SignHTTP(ctx, creds, req, payloadHash, t.service, t.region, time.Now()); err != nil {
		return nil, fmt.Errorf("sigv4: sign request: %w", err)
	}

	return t.base.RoundTrip(req)
}

// hashBody reads the body (if any), computes its SHA-256 digest, and restores
// the body so downstream transports can still read it. Returns
// emptyPayloadHash for nil/empty bodies and unsignedPayloadValue when the
// WithUnsignedPayload option is set.
func (t *sigv4Transport) hashBody(req *http.Request) (string, error) {
	if t.cfg.unsignedPayload {
		return unsignedPayloadValue, nil
	}
	if req.Body == nil || req.Body == http.NoBody {
		return emptyPayloadHash, nil
	}

	data, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return "", err
	}

	req.Body = io.NopCloser(bytes.NewReader(data))
	req.ContentLength = int64(len(data))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}
