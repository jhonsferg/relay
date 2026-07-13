module github.com/jhonsferg/relay/examples

go 1.25.0

require (
	github.com/alicebob/miniredis/v2 v2.38.0
	github.com/jhonsferg/relay v0.4.8
	github.com/jhonsferg/relay/ext/metrics v0.2.1
	github.com/jhonsferg/relay/ext/oauth v0.1.3
	github.com/jhonsferg/relay/ext/prometheus v0.1.3
	github.com/jhonsferg/relay/ext/redis v0.1.2
	github.com/jhonsferg/relay/ext/tracing v0.2.2
	github.com/jhonsferg/relay/ext/zap v0.1.2
	github.com/prometheus/client_golang v1.23.2
	github.com/redis/go-redis/v9 v9.21.0
	go.opentelemetry.io/otel v1.44.0
	go.opentelemetry.io/otel/metric v1.44.0
	go.opentelemetry.io/otel/trace v1.44.0
	go.uber.org/zap v1.28.0
)

require (
	github.com/andybalholm/brotli v1.2.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/klauspost/compress v1.19.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	github.com/yuin/gopher-lua v1.1.2 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/jhonsferg/relay v0.1.1 => ../
	github.com/jhonsferg/relay/ext/metrics v0.1.1 => ../ext/metrics
	github.com/jhonsferg/relay/ext/oauth v0.1.1 => ../ext/oauth
	github.com/jhonsferg/relay/ext/prometheus v0.1.1 => ../ext/prometheus
	github.com/jhonsferg/relay/ext/redis v0.1.1 => ../ext/redis
	github.com/jhonsferg/relay/ext/tracing v0.1.1 => ../ext/tracing
	github.com/jhonsferg/relay/ext/zap v0.1.1 => ../ext/zap
)
