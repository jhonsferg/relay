# VCR - HTTP Cassette Recording and Playback

Package vcr provides HTTP interaction recording and playback for testing. Inspired by the [VCR gem](https://github.com/vcr/vcr) from Ruby, it allows you to record real HTTP requests to a cassette file and replay them in subsequent test runs without hitting the real server.

## Features

- **Record Mode**: Execute real requests and save interactions to a cassette file
- **Playback Mode**: Replay recorded interactions without hitting the real server
- **Passthrough Mode**: Disable VCR and allow real requests to pass through
- **Sequential Matching**: Matches interactions by method and URL in order
- **JSON Format**: Cassettes are stored as human-readable JSON files
- **Easy Integration**: Works as relay middleware

## Installation

```bash
go get github.com/jhonsferg/relay/ext/vcr
```

## Usage

### Basic Example

```go
package main

import (
	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/vcr"
)

func main() {
	// Create a VCR in record mode
	recorder, err := vcr.New("cassettes/example.json", vcr.ModeRecord)
	if err != nil {
		panic(err)
	}

	// Create a relay client with VCR middleware
	client := relay.New(
		relay.WithBaseURL("https://api.example.com"),
		recorder.Middleware(),
	)

	// Make requests - they will be recorded to the cassette
	resp, err := client.Get(nil, "/users")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	// Save the cassette when done
	if err := recorder.Save(); err != nil {
		panic(err)
	}
}
```

### Testing with Playback

```go
func TestUserAPI(t *testing.T) {
	// Use playback mode in tests
	player, err := vcr.New("cassettes/example.json", vcr.ModePlayback)
	if err != nil {
		t.Fatalf("Failed to load cassette: %v", err)
	}

	client := relay.New(
		relay.WithBaseURL("https://api.example.com"),
		player.Middleware(),
	)

	// Requests are replayed from the cassette
	resp, err := client.Get(nil, "/users")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Assert response...
}
```

## Modes

### ModeRecord
Execute real HTTP requests and record them to a cassette file. The cassette is saved after each request, so if a test fails, previous requests are still recorded.

### ModePlayback
Replay recorded interactions from a cassette file without making any real HTTP requests. If a matching interaction is not found, an error is returned. Interactions are matched by method and URL in the order they appear in the cassette.

### ModePassthrough
Disable VCR and allow all requests to pass through to the real server. Useful for temporarily disabling cassettes without removing the middleware.

## Cassette Format

Cassettes are stored as JSON files with the following structure:

```json
{
  "interactions": [
    {
      "request": {
        "method": "GET",
        "url": "https://api.example.com/users",
        "header": {
          "Authorization": "Bearer token"
        },
        "body": "",
        "body_encoding": "base64"
      },
      "response": {
        "status": 200,
        "header": {
          "Content-Type": "application/json"
        },
        "body": "W3siaWQiOjEsIm5hbWUiOiJKb2huIn1d",
        "body_encoding": "base64"
      }
    }
  ]
}
```

## Notes

- In record mode, the cassette is saved after each request for the first 20
  interactions, and every 10th interaction after that, so a large recording
  session (hundreds of interactions) doesn't pay the cost of rewriting the
  entire cassette file on every single request. Always call `recorder.Save()`
  when the recording session ends (as in the example above) to flush any
  interactions recorded since the last automatic save.
- In playback mode, interactions are matched sequentially (first match wins)
- Request and response bodies are stored base64-encoded in the cassette so
  binary bodies (images, protobuf, gzip) round-trip correctly. Cassettes
  written by earlier versions of this package (plain-text bodies, no
  `body_encoding` field) still load correctly.
- Only the first value of multi-valued headers is recorded
- Cassettes are human-readable and can be edited manually if needed
