module github.com/jhonsferg/relay/ext/breaker/gobreaker

go 1.24.0

require (
	github.com/jhonsferg/relay v0.1.1
	github.com/sony/gobreaker v1.0.0
)

replace github.com/jhonsferg/relay v0.1.1 => ../../../

require (
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/sync v0.16.0 // indirect
)
