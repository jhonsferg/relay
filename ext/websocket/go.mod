module github.com/jhonsferg/relay/ext/websocket

go 1.24.0

require (
	github.com/gorilla/websocket v1.5.3
	github.com/jhonsferg/relay v0.1.11
)

require golang.org/x/sync v0.16.0 // indirect

replace github.com/jhonsferg/relay v0.1.11 => ../../
