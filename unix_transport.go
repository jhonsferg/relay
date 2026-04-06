package relay

// WithUnixSocket configures the relay client to connect via a Unix domain socket.
// All requests will be routed through the socket at socketPath, regardless of
// the host in the request URL.
//
// The baseURL still controls the HTTP host header and path; only the network
// transport layer is changed to use the Unix socket.
//
// Example:
//
//	client := relay.New(
//	    relay.WithBaseURL("http://localhost"),
//	    relay.WithUnixSocket("/var/run/docker.sock"),
//	)
func WithUnixSocket(socketPath string) Option {
	return func(c *Config) {
		c.UnixSocketPath = socketPath
	}
}
