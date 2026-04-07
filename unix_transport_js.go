//go:build js

package relay

// WithUnixSocket is a no-op on js/wasm targets; Unix domain sockets are not
// supported by the browser Fetch API. The option is accepted to keep call
// sites portable, but the socket path is silently ignored.
func WithUnixSocket(_ string) Option {
	return func(_ *Config) {}
}
