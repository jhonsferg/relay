module github.com/jhonsferg/relay/ext/redis

go 1.24.0

require (
	github.com/alicebob/miniredis/v2 v2.37.0
	github.com/jhonsferg/relay v0.1.1
	github.com/redis/go-redis/v9 v9.19.0
)

replace github.com/jhonsferg/relay v0.1.1 => ../../

require (
	github.com/andybalholm/brotli v1.2.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/yuin/gopher-lua v1.1.2 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
)
