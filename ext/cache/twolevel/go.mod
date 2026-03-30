module github.com/jhonsferg/relay/ext/cache/twolevel

go 1.24

require (
	github.com/jhonsferg/relay v0.0.0
	github.com/jhonsferg/relay/ext/cache/lru v0.0.0
)

replace (
	github.com/jhonsferg/relay v0.0.0 => ../../../
	github.com/jhonsferg/relay/ext/cache/lru v0.0.0 => ../lru
)

require golang.org/x/sync v0.16.0 // indirect
