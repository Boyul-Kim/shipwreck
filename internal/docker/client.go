package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"shipwreck/internal/dial"
	"time"
)

const apiVersion = "v1.41"

// A connection that can be bounded by the caller's context. Unix sockets and
// TCP satisfy this; a Windows named pipe opened as an *os.File may not, so the
// deadline is best effort.
type deadliner interface {
	SetDeadline(time.Time) error
}

// Sends one request and hands back the live connection so the caller can read
// the body before closing it. The caller owns both returns.
func send(ctx context.Context, method string, path string, body any) (io.ReadWriteCloser, *http.Response, error) {
	var payload io.Reader

	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("encoding request: %w", err)
		}

		payload = bytes.NewReader(encoded)
	}

	host := dial.DefaultHost()
	conn, err := dial.Dial(ctx, host)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to %s: %w", host, err)
	}

	if d, ok := ctx.Deadline(); ok {
		if c, ok := conn.(deadliner); ok {
			_ = c.SetDeadline(d)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://docker/"+apiVersion+path, payload)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("writing request: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("reading response: %w", err)
	}

	// A status like kill's 204 wouldn't pass a plain StatusOK check.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		resp.Body.Close()
		conn.Close()

		return nil, nil, fmt.Errorf("docker api: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}

	return conn, resp, nil
}

func do(ctx context.Context, method string, path string, body any, v any) error {
	conn, resp, err := send(ctx, method, path, body)
	if err != nil {
		return err
	}

	defer conn.Close()
	defer resp.Body.Close()

	// The body has to be read before the deferred conn.Close() runs, so the
	// decode belongs here rather than in the caller.
	if v == nil {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

func get(ctx context.Context, path string, v any) error {
	return do(ctx, http.MethodGet, path, nil, v)
}

func post(ctx context.Context, path string, body any, v any) error {
	return do(ctx, http.MethodPost, path, body, v)
}
