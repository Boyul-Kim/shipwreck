//go:build unix

package dial

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

/*
*

	Returns first Docker socket that exists

*
*/
func defaultHost() string {
	const standard = "/var/run/docker.sock"
	candidates := []string{standard}

	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		//rootless
		candidates = append(candidates, filepath.Join(dir, "docker.sock"))
	}

	if home, err := os.UserHomeDir(); err == nil {
		//docker desktop
		candidates = append(candidates, filepath.Join(home, ".docker", "run", "docker.sock"))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return "unix://" + p
		}
	}

	return "unix://" + standard
}

func dialPipe(context.Context, string) (io.ReadWriteCloser, error) {
	return nil, errors.New("npipe:// hosts are only supported on Windows")
}
