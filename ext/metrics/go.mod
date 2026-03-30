module github.com/jhonsferg/relay/ext/metrics

go 1.24

require (
	github.com/jhonsferg/relay v0.0.0
	go.opentelemetry.io/otel v1.42.0
	go.opentelemetry.io/otel/metric v1.42.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/trace v1.42.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
)

replace github.com/jhonsferg/relay v0.0.0 => ../../
