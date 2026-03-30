module github.com/jhonsferg/relay/ext/redis

go 1.25.0

require (
	github.com/alicebob/miniredis/v2 v2.33.0
	github.com/jhonsferg/relay v0.1.0
	github.com/redis/go-redis/v9 v9.18.0
)

replace github.com/jhonsferg/relay v0.1.0 => ../../

require (
	github.com/alicebob/gopher-json v0.1.0-20230218143504-906a9b012302 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.1.0-20200823014737-9f7001d12a5f // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
)
