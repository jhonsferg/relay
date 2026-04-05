# AWS SigV4 Extension

The AWS SigV4 extension signs every outgoing relay request using AWS Signature Version 4. This enables relay clients to call AWS services directly - S3, DynamoDB, API Gateway, Lambda function URLs, and any other AWS API - using the same relay middleware stack you use for external HTTP APIs.

**Import path:** `github.com/jhonsferg/relay/ext/sigv4`

---

## Overview

AWS Signature Version 4 (SigV4) is the authentication protocol required by nearly all AWS services. Signing involves computing an HMAC-SHA256 signature over a canonical request that includes the HTTP method, URL, headers, and body. The signature is included in the `Authorization` request header.

The extension handles the full SigV4 signing process transparently:
- Canonical request construction
- String-to-sign computation
- Signing key derivation (date + region + service + request key)
- `Authorization`, `X-Amz-Date`, and `X-Amz-Security-Token` header injection
- Automatic credential refresh when using session-based credentials (IAM roles, ECS task roles, EC2 instance profiles)

---

## Installation

```bash
go get github.com/jhonsferg/relay/ext/sigv4@latest
go get github.com/aws/aws-sdk-go-v2@latest
go get github.com/aws/aws-sdk-go-v2/config@latest
go get github.com/aws/aws-sdk-go-v2/credentials@latest
```

---

## Options

### `relaysigv4.WithAWSSigV4`

```go
relaysigv4.WithAWSSigV4(region, service string, creds aws.CredentialsProvider) relay.Option
```

Attaches the SigV4 signing middleware to a relay client.

- `region` - the AWS region where the target service is deployed (e.g., `"us-east-1"`)
- `service` - the AWS service signing name (e.g., `"s3"`, `"execute-api"`, `"lambda"`)
- `creds` - any `aws.CredentialsProvider` implementation

You can find the service signing name in the [AWS documentation](https://docs.aws.amazon.com/IAM/latest/UserGuide/reference_policies_actions-resources-contextkeys.html) or in the SDK's service package constants.

---

## Credential Providers

### Static Credentials

Use `credentials.NewStaticCredentialsProvider` when you have a fixed access key and secret. This is common in local development and CI environments where secrets are injected as environment variables.

```go
import (
    "github.com/aws/aws-sdk-go-v2/credentials"
    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
)

creds := credentials.NewStaticCredentialsProvider(
    "AKIAIOSFODNN7EXAMPLE",          // AWS_ACCESS_KEY_ID
    "wJalrXUtnFEMI/K7MDENG/bPxRfi", // AWS_SECRET_ACCESS_KEY
    "",                              // session token (empty for long-term credentials)
)

client, _ := relay.NewClient(
    relay.WithBaseURL("https://s3.us-east-1.amazonaws.com"),
    relaysigv4.WithAWSSigV4("us-east-1", "s3", creds),
)
```

> **Warning:** Never hard-code access keys in source code. Load them from environment variables, AWS Secrets Manager, or your CI/CD platform's secret management system.

### Environment Variable Credentials

The standard AWS SDK v2 config loader reads credentials from the environment automatically:

```go
import (
    "context"
    "github.com/aws/aws-sdk-go-v2/config"
    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
)

// Reads from: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN,
// AWS_PROFILE, AWS_CONFIG_FILE, and the default credentials file.
cfg, err := config.LoadDefaultConfig(context.Background(),
    config.WithRegion("us-east-1"),
)
if err != nil {
    log.Fatalf("load aws config: %v", err)
}

client, _ := relay.NewClient(
    relay.WithBaseURL("https://s3.us-east-1.amazonaws.com"),
    relaysigv4.WithAWSSigV4("us-east-1", "s3", cfg.Credentials),
)
```

This is the recommended approach for production code because it supports the full AWS credentials chain without any code changes between local development (where developers use `~/.aws/credentials`) and production (where services use IAM roles).

### IAM Role Credentials (EC2 Instance Profile / ECS Task Role / EKS Pod Identity)

IAM roles are the recommended credential source for workloads running on AWS infrastructure. The SDK automatically retrieves and refreshes temporary credentials from the instance metadata service (IMDS) or the ECS container credentials endpoint.

```go
import (
    "context"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials/ec2rolecreds"
    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
)

// EC2 Instance Profile
cfg, err := config.LoadDefaultConfig(context.Background(),
    config.WithRegion("us-east-1"),
    // The SDK uses IMDS automatically; no extra config needed on EC2.
)
if err != nil {
    log.Fatalf("load config: %v", err)
}

// If you need to explicitly use the EC2 instance profile provider:
ec2Creds := ec2rolecreds.New()

client, _ := relay.NewClient(
    relay.WithBaseURL("https://dynamodb.us-east-1.amazonaws.com"),
    relaysigv4.WithAWSSigV4("us-east-1", "dynamodb", ec2Creds),
)
```

```go
// ECS Task Role (also works for EKS with IRSA / EKS Pod Identity)
import (
    "context"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials/endpointcreds"
    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
    "os"
)

// ECS injects AWS_CONTAINER_CREDENTIALS_RELATIVE_URI or
// AWS_CONTAINER_CREDENTIALS_FULL_URI into the task environment.
// config.LoadDefaultConfig picks this up automatically.
cfg, err := config.LoadDefaultConfig(context.Background())
if err != nil {
    log.Fatalf("load config: %v", err)
}

region := os.Getenv("AWS_REGION") // set in ECS task definition
client, _ := relay.NewClient(
    relay.WithBaseURL("https://execute-api."+region+".amazonaws.com"),
    relaysigv4.WithAWSSigV4(region, "execute-api", cfg.Credentials),
)
```

> **Note:** Temporary credentials (session tokens) from IAM roles have a limited lifetime, typically 1 to 12 hours. The AWS SDK automatically refreshes these credentials before they expire. The relay SigV4 extension re-reads credentials for every request, so refreshed credentials are always used.

---

## Complete Example: Calling AWS S3 REST API

This example retrieves an object from S3 using the S3 REST API directly, without the AWS SDK's S3 client:

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"
    "os"
    "time"

    relay "github.com/jhonsferg/relay"
    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
    "github.com/aws/aws-sdk-go-v2/config"
)

func main() {
    bucket := os.Getenv("S3_BUCKET")
    region := os.Getenv("AWS_REGION")
    if bucket == "" || region == "" {
        log.Fatal("S3_BUCKET and AWS_REGION must be set")
    }

    cfg, err := config.LoadDefaultConfig(context.Background(),
        config.WithRegion(region),
    )
    if err != nil {
        log.Fatalf("load aws config: %v", err)
    }

    // S3 virtual-hosted-style URL: https://<bucket>.s3.<region>.amazonaws.com
    baseURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com", bucket, region)

    client, err := relay.NewClient(
        relay.WithBaseURL(baseURL),
        relaysigv4.WithAWSSigV4(region, "s3", cfg.Credentials),
        relay.WithTimeout(30*time.Second),
    )
    if err != nil {
        log.Fatalf("create client: %v", err)
    }

    ctx := context.Background()

    // GET object - returns the raw response body.
    // Using client.GetRaw to handle binary content.
    resp, err := client.GetRaw(ctx, "/data/sample.json")
    if err != nil {
        log.Fatalf("get object: %v", err)
    }
    defer resp.Body.Close()

    fmt.Printf("Status: %s\n", resp.Status)
    fmt.Printf("Content-Type: %s\n", resp.Header.Get("Content-Type"))
    fmt.Printf("Content-Length: %s\n", resp.Header.Get("Content-Length"))
    fmt.Printf("ETag: %s\n", resp.Header.Get("ETag"))
    fmt.Printf("Last-Modified: %s\n", resp.Header.Get("Last-Modified"))

    body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
    if err != nil {
        log.Fatalf("read body: %v", err)
    }
    fmt.Printf("\nBody (first 4096 bytes):\n%s\n", body)
}
```

### Listing Objects in a Bucket

```go
package main

import (
    "context"
    "encoding/xml"
    "fmt"
    "log"
    "os"

    relay "github.com/jhonsferg/relay"
    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
    "github.com/aws/aws-sdk-go-v2/config"
)

type ListBucketResult struct {
    XMLName     xml.Name  `xml:"ListBucketResult"`
    Name        string    `xml:"Name"`
    Prefix      string    `xml:"Prefix"`
    MaxKeys     int       `xml:"MaxKeys"`
    IsTruncated bool      `xml:"IsTruncated"`
    Contents    []S3Object `xml:"Contents"`
}

type S3Object struct {
    Key          string `xml:"Key"`
    LastModified string `xml:"LastModified"`
    ETag         string `xml:"ETag"`
    Size         int64  `xml:"Size"`
    StorageClass string `xml:"StorageClass"`
}

func main() {
    bucket := os.Getenv("S3_BUCKET")
    region := os.Getenv("AWS_REGION")

    cfg, _ := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))

    baseURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com", bucket, region)
    client, _ := relay.NewClient(
        relay.WithBaseURL(baseURL),
        relaysigv4.WithAWSSigV4(region, "s3", cfg.Credentials),
        // S3 returns XML by default; use the XML decoder option.
        relay.WithResponseDecoder(relay.XMLDecoder),
    )

    var result ListBucketResult
    // List up to 10 objects with prefix "data/".
    if err := client.Get(context.Background(), "/?list-type=2&max-keys=10&prefix=data%2F", &result); err != nil {
        log.Fatalf("list objects: %v", err)
    }

    fmt.Printf("Bucket: %s  (truncated: %v)\n", result.Name, result.IsTruncated)
    for _, obj := range result.Contents {
        fmt.Printf("  %-60s  %8d bytes  %s\n", obj.Key, obj.Size, obj.LastModified[:10])
    }
}
```

---

## Complete Example: Calling API Gateway

Amazon API Gateway endpoints require SigV4 signing when IAM authorization is enabled on the route.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    relay "github.com/jhonsferg/relay"
    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
    "github.com/aws/aws-sdk-go-v2/config"
)

type OrderRequest struct {
    ProductID string `json:"product_id"`
    Quantity  int    `json:"quantity"`
    UserID    string `json:"user_id"`
}

type OrderResponse struct {
    OrderID   string `json:"order_id"`
    Status    string `json:"status"`
    CreatedAt string `json:"created_at"`
    Total     float64 `json:"total"`
}

func main() {
    region := os.Getenv("AWS_REGION")
    apiID  := os.Getenv("API_GATEWAY_ID")  // e.g., "abc123xyz"
    stage  := os.Getenv("API_GATEWAY_STAGE") // e.g., "prod"

    if region == "" || apiID == "" || stage == "" {
        log.Fatal("AWS_REGION, API_GATEWAY_ID, and API_GATEWAY_STAGE must be set")
    }

    cfg, err := config.LoadDefaultConfig(context.Background(),
        config.WithRegion(region),
    )
    if err != nil {
        log.Fatalf("load config: %v", err)
    }

    // API Gateway REST API URL format:
    // https://{api-id}.execute-api.{region}.amazonaws.com/{stage}
    baseURL := fmt.Sprintf("https://%s.execute-api.%s.amazonaws.com/%s", apiID, region, stage)

    client, err := relay.NewClient(
        relay.WithBaseURL(baseURL),
        // Service name for API Gateway is "execute-api"
        relaysigv4.WithAWSSigV4(region, "execute-api", cfg.Credentials),
        relay.WithTimeout(15*time.Second),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts:     3,
            WaitBase:        500 * time.Millisecond,
            RetryableStatus: []int{429, 500, 502, 503, 504},
        }),
    )
    if err != nil {
        log.Fatalf("create client: %v", err)
    }

    ctx := context.Background()

    // Create an order via the API Gateway endpoint.
    var orderResp OrderResponse
    if err := client.Post(ctx, "/orders", &OrderRequest{
        ProductID: "prod-abc-123",
        Quantity:  2,
        UserID:    "user-456",
    }, &orderResp); err != nil {
        log.Fatalf("create order: %v", err)
    }

    fmt.Printf("Order created:\n")
    fmt.Printf("  ID:         %s\n", orderResp.OrderID)
    fmt.Printf("  Status:     %s\n", orderResp.Status)
    fmt.Printf("  Created at: %s\n", orderResp.CreatedAt)
    fmt.Printf("  Total:      $%.2f\n", orderResp.Total)

    // Get order status.
    var statusResp OrderResponse
    if err := client.Get(ctx, "/orders/"+orderResp.OrderID, &statusResp); err != nil {
        log.Fatalf("get order: %v", err)
    }
    fmt.Printf("Order status: %s\n", statusResp.Status)
}
```

---

## Content-SHA256 Header

For S3 operations that include a request body, S3 requires the `X-Amz-Content-SHA256` header. The extension computes and injects this header automatically. For requests without a body (GET, HEAD, DELETE), the extension uses the literal string `UNSIGNED-PAYLOAD` for services that allow it (like CloudFront) or the SHA256 of an empty string for services that require it (like S3).

Override the payload hash behavior for specific cases:

```go
relaysigv4.WithAWSSigV4(region, "s3", creds,
    relaysigv4.WithUnsignedPayload(), // Use UNSIGNED-PAYLOAD for all requests
)
```

---

## Assuming a Role with STS

To assume an IAM role before making requests, use the `stscreds` package:

```go
import (
    "context"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials/stscreds"
    "github.com/aws/aws-sdk-go-v2/service/sts"
    relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
)

cfg, _ := config.LoadDefaultConfig(context.Background(), config.WithRegion("us-east-1"))
stsClient := sts.NewFromConfig(cfg)

// Assume a cross-account role.
roleProvider := stscreds.NewAssumeRoleProvider(stsClient,
    "arn:aws:iam::123456789012:role/MyRelayRole",
    func(o *stscreds.AssumeRoleOptions) {
        o.RoleSessionName = "relay-session"
        o.Duration = 3600 // 1 hour
    },
)

client, _ := relay.NewClient(
    relay.WithBaseURL("https://s3.us-east-1.amazonaws.com"),
    relaysigv4.WithAWSSigV4("us-east-1", "s3", roleProvider),
)
```

---

## See Also

- [OAuth2 Extension](oauth.md) - alternative authentication for non-AWS APIs
- [Mock Transport Extension](mock.md) - unit testing signed AWS requests
- relay core documentation - retry and circuit breaking for AWS API calls
