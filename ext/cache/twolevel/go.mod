module github.com/jhonsferg/relay/ext/cache/twolevel

go 1.25.0

require (
	github.com/jhonsferg/relay v0.4.3
	github.com/jhonsferg/relay/ext/cache/lru v0.1.0
)

replace github.com/jhonsferg/relay/ext/cache/lru v0.1.0 => ../lru

require (
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/klauspost/compress v1.20.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
)
