package dial

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"time"
)

const errorPipeBusy = syscall.Errno(231)

func defaultHost() string {
	return "npipe:////./pipe/docker_engine"
}

func dialPipe(ctx context.Context, path string) (io.ReadWriteCloser, error) {
	for {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err == nil {
			return f, nil
		}

		if !errors.Is(err, errorPipeBusy) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
