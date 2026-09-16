package dial

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
)

func DefaultHost() string {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h
	}

	return defaultHost()
}

func Dial(ctx context.Context, host string) (io.ReadWriteCloser, error) {
	scheme, addr, ok := strings.Cut(host, "://")
	if !ok {
		return nil, fmt.Errorf("dial: malformed host %q", host)
	}

	switch scheme {
	case "unix":
		return dialNet(ctx, "unix", addr)

	case "npipe":
		return dialPipe(ctx, filepath.FromSlash(addr))

	case "tcp", "http":
		return dialNet(ctx, "tcp", addr)

	default:
		return nil, fmt.Errorf("dial: unsupported scheme %q in host %q", scheme, host)
	}
}

func dialNet(ctx context.Context, network, addr string) (io.ReadWriteCloser, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}
