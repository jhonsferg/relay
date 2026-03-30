module github.com/jhonsferg/relay/ext/cache/twolevel

go 1.24.0

require (
	github.com/jhonsferg/relay v0.1.0
	github.com/jhonsferg/relay/ext/cache/lru v0.1.0
)

replace (
	github.com/jhonsferg/relay v0.1.0 => ../../../
	github.com/jhonsferg/relay/ext/cache/lru v0.1.0 => ../lru
)

require golang.org/x/sync v0.16.0 // indirect
