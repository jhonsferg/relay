module github.com/jhonsferg/relay/ext/redis

go 1.25.0

require (
	github.com/alicebob/miniredis/v2 v2.33.0
	github.com/jhonsferg/relay v0.0.0
	github.com/redis/go-redis/v9 v9.7.3
)

replace github.com/jhonsferg/relay v0.0.0 => ../../

require (
	github.com/alicebob/gopher-json v0.0.0-20230218143504-906a9b012302 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	golang.org/x/sync v0.13.0 // indirect
)
