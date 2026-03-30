module github.com/jhonsferg/relay/examples

go 1.25.0

require (
	github.com/jhonsferg/relay v0.0.0
	github.com/jhonsferg/relay/ext/metrics v0.0.0
	github.com/jhonsferg/relay/ext/oauth v0.0.0
	github.com/jhonsferg/relay/ext/prometheus v0.0.0
	github.com/jhonsferg/relay/ext/redis v0.0.0
	github.com/jhonsferg/relay/ext/tracing v0.0.0
	github.com/jhonsferg/relay/ext/zap v0.0.0
	github.com/jhonsferg/relay/ext/zerolog v0.0.0
	github.com/prometheus/client_golang v1.23.2
	github.com/redis/go-redis/v9 v9.18.0
	github.com/rs/zerolog v1.35.0
	go.opentelemetry.io/otel v1.42.0
	go.opentelemetry.io/otel/metric v1.42.0
	go.opentelemetry.io/otel/trace v1.42.0
	go.uber.org/zap v1.27.1
)

replace (
	github.com/jhonsferg/relay v0.0.0 => ../
	github.com/jhonsferg/relay/ext/metrics v0.0.0 => ../ext/metrics
	github.com/jhonsferg/relay/ext/oauth v0.0.0 => ../ext/oauth
	github.com/jhonsferg/relay/ext/prometheus v0.0.0 => ../ext/prometheus
	github.com/jhonsferg/relay/ext/redis v0.0.0 => ../ext/redis
	github.com/jhonsferg/relay/ext/tracing v0.0.0 => ../ext/tracing
	github.com/jhonsferg/relay/ext/zap v0.0.0 => ../ext/zap
	github.com/jhonsferg/relay/ext/zerolog v0.0.0 => ../ext/zerolog
)
