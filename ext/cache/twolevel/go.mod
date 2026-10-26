module github.com/jhonsferg/relay/ext/cache/twolevel

go 1.26.0

require (
	github.com/jhonsferg/relay v0.6.1
	github.com/jhonsferg/relay/ext/cache/lru v0.1.0
)

replace github.com/jhonsferg/relay/ext/cache/lru v0.1.0 => ../lru

require (
	github.com/andybalholm/brotli v1.2.6 // indirect
	github.com/klauspost/compress v1.20.3 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)
