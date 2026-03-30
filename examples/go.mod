module github.com/jhonsferg/relay/examples

go 1.25.0

require (
	github.com/alicebob/miniredis/v2 v2.33.0
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

require (
	github.com/alicebob/gopher-json v0.0.0-20230218143504-906a9b012302 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/sys v0.35.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
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
