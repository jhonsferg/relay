//go:build docker

// Integration test against a real S3-compatible server (MinIO), run via
// testcontainers-go. Opt-in (requires a local Docker daemon), skipped by
// default - run with `go test -tags=docker ./...`.
//
// sigv4_test.go only checks that the Authorization header is present and
// shaped correctly (contains "AWS4-HMAC-SHA256", the expected credential
// scope, etc.) - it never proves the signature is actually cryptographically
// correct. MinIO implements strict AWS SigV4 signature verification (its
// entire access-control model depends on it), so a request that MinIO
// accepts is proof the signing implementation produces a valid signature; a
// request signed with the wrong secret key must be rejected, which this test
// also verifies to confirm MinIO is genuinely validating and not just
// accepting anything.
//
// A real AWS-hosted S3 bucket (or LocalStack) was considered instead, but
// LocalStack now requires a paid license/auth token even for its community
// S3 service in the image available at the time this was written, and a
// real AWS account is out of scope for an automated, credential-free test
// suite. MinIO is fully open source and needs neither.
package sigv4_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/jhonsferg/relay"
	relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
)

const (
	dockerTestAccessKey = "relaytestaccesskey"
	dockerTestSecretKey = "relaytestsecretkey123"
)

// startMinIO starts a real MinIO container (a genuine S3-API-compatible,
// SigV4-verifying server) and returns its HTTP endpoint. The container is
// terminated when the test finishes.
func startMinIO(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	container, err := tcminio.Run(ctx, "minio/minio:latest",
		tcminio.WithUsername(dockerTestAccessKey),
		tcminio.WithPassword(dockerTestSecretKey),
	)
	if err != nil {
		t.Fatalf("tcminio.Run: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("container.Terminate: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}
	return "http://" + connStr
}

func TestDocker_WithSigV4_AcceptedByRealS3Server(t *testing.T) {
	endpoint := startMinIO(t)

	creds := credentials.NewStaticCredentialsProvider(dockerTestAccessKey, dockerTestSecretKey, "")
	client := relay.New(
		relay.WithBaseURL(endpoint),
		relaysigv4.WithSigV4(&creds, "s3", "us-east-1"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(10*time.Second),
	)

	// ListBuckets (GET /) is signed and sent to a real S3-API server. MinIO
	// validates the SigV4 signature before processing the request - a 200
	// here proves relay produced a signature MinIO's own verification logic
	// accepts as cryptographically correct, not just header-shaped.
	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200 (body: %s)", resp.StatusCode, resp.String())
	}
}

func TestDocker_WithSigV4_WrongSecretIsRejected(t *testing.T) {
	endpoint := startMinIO(t)

	// Correct access key, deliberately wrong secret key - the resulting
	// signature will not match what MinIO computes server-side.
	creds := credentials.NewStaticCredentialsProvider(dockerTestAccessKey, "not-the-real-secret-key", "")
	client := relay.New(
		relay.WithBaseURL(endpoint),
		relaysigv4.WithSigV4(&creds, "s3", "us-east-1"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(10*time.Second),
	)

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// This is the control case proving the prior test's 200 means something:
	// if MinIO accepted every request regardless of signature, this would
	// also return 200, and the signing implementation would never have been
	// meaningfully verified.
	if resp.StatusCode == http.StatusOK {
		t.Error("expected a wrong secret key to be rejected, but MinIO returned 200 - " +
			"either MinIO isn't validating signatures, or something is silently ignoring credentials")
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Logf("wrong-secret request rejected with status %d (not the typical 403, but not 200 either): %s", resp.StatusCode, resp.String())
	}
}
