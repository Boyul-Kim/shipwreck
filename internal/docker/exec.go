package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// captureLimit bounds how much of an exec's output is held in memory. The rest
// is still read, because a process whose stdout stops draining blocks.
const captureLimit = 64 << 10

// frameChunk is how much of one stream frame is read at a time.
const frameChunk = 32 << 10

type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Output is stdout, or stderr when the command printed nothing else -- which is
// the interesting half of a failed exec.
func (r ExecResult) Output() string {
	out := strings.TrimRight(r.Stdout, "\n")
	errOut := strings.TrimRight(r.Stderr, "\n")

	switch {
	case out == "":
		return errOut

	case errOut == "":
		return out
	}

	return out + "\n" + errOut
}

/*
*

	Exec runs one command in a running container and waits for it, returning its
	exit code and output.

	Do not hand this a command that does not return; use ExecDetached for those.
	The Engine API holds the stream open for as long as the process lives, so a
	spin loop would block until the context deadline.

*
*/
func Exec(ctx context.Context, id string, cmd []string) (ExecResult, error) {
	execID, err := execCreate(ctx, id, cmd, false)
	if err != nil {
		return ExecResult{}, err
	}

	stdout, stderr, err := execAttach(ctx, execID)
	if err != nil {
		return ExecResult{}, err
	}

	code, err := execExitCode(ctx, execID)
	if err != nil {
		return ExecResult{Stdout: stdout, Stderr: stderr}, err
	}

	return ExecResult{ExitCode: code, Stdout: stdout, Stderr: stderr}, nil
}

// ExecDetached starts a command and returns as soon as the daemon has it. The
// process keeps running after shipwreck's request is done, which is what a CPU
// burner needs.
func ExecDetached(ctx context.Context, id string, cmd []string) (string, error) {
	execID, err := execCreate(ctx, id, cmd, true)
	if err != nil {
		return "", err
	}

	body := struct {
		Detach bool
		Tty    bool
	}{Detach: true}

	if err := post(ctx, "/exec/"+url.PathEscape(execID)+"/start", body, nil); err != nil {
		return "", fmt.Errorf("error starting exec in %s: %w", short(id), err)
	}

	return execID, nil
}

// Shell runs a /bin/sh script, which is how every in-container fault is
// expressed: redirection, loops and fallbacks all need a shell.
func Shell(ctx context.Context, id string, script string) (ExecResult, error) {
	return Exec(ctx, id, []string{"/bin/sh", "-c", script})
}

func ShellDetached(ctx context.Context, id string, script string) (string, error) {
	return ExecDetached(ctx, id, []string{"/bin/sh", "-c", script})
}

func execCreate(ctx context.Context, id string, cmd []string, detach bool) (string, error) {
	body := struct {
		AttachStdin  bool
		AttachStdout bool
		AttachStderr bool
		Tty          bool
		Cmd          []string
	}{
		AttachStdout: !detach,
		AttachStderr: !detach,
		Cmd:          cmd,
	}

	var out struct {
		ID string `json:"Id"`
	}

	path := "/containers/" + url.PathEscape(id) + "/exec"
	if err := post(ctx, path, body, &out); err != nil {
		return "", fmt.Errorf("error creating exec in %s: %w", short(id), err)
	}

	if out.ID == "" {
		return "", fmt.Errorf("error creating exec in %s: daemon returned no exec id", short(id))
	}

	return out.ID, nil
}

// Starts an attached exec and reads the hijacked stream to the end. The daemon
// keeps the connection open for the life of the process, so this returns when
// the command exits.
func execAttach(ctx context.Context, execID string) (string, string, error) {
	body := struct {
		Detach bool
		Tty    bool
	}{}

	conn, resp, err := send(ctx, "POST", "/exec/"+url.PathEscape(execID)+"/start", body)
	if err != nil {
		return "", "", fmt.Errorf("error starting exec: %w", err)
	}

	defer conn.Close()
	defer resp.Body.Close()

	stdout, stderr, err := demux(resp.Body)
	if err != nil {
		return stdout, stderr, fmt.Errorf("error reading exec output: %w", err)
	}

	return stdout, stderr, nil
}

func execExitCode(ctx context.Context, execID string) (int, error) {
	var out struct {
		ExitCode int  `json:"ExitCode"`
		Running  bool `json:"Running"`
	}

	if err := get(ctx, "/exec/"+url.PathEscape(execID)+"/json", &out); err != nil {
		return 0, fmt.Errorf("error inspecting exec: %w", err)
	}

	return out.ExitCode, nil
}

/*
*

	Splits Docker's multiplexed stream into stdout and stderr.

	Every frame is an 8 byte header -- stream number, three pad bytes, then a big
	endian length -- followed by that many bytes of payload. The frame is always
	read in full even once the capture limit is reached, since an undrained pipe
	stalls the process on the other end.

*
*/
func demux(r io.Reader) (string, string, error) {
	var stdout, stderr bytes.Buffer

	header := make([]byte, 8)
	chunk := make([]byte, frameChunk)

	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return stdout.String(), stderr.String(), nil
			}

			return stdout.String(), stderr.String(), err
		}

		sink := &stdout
		if header[0] == 2 {
			sink = &stderr
		}

		remaining := int64(binary.BigEndian.Uint32(header[4:8]))

		for remaining > 0 {
			size := int64(len(chunk))
			if remaining < size {
				size = remaining
			}

			n, err := io.ReadFull(r, chunk[:size])
			if n > 0 && sink.Len() < captureLimit {
				sink.Write(chunk[:n])
			}

			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					return stdout.String(), stderr.String(), nil
				}

				return stdout.String(), stderr.String(), err
			}

			remaining -= int64(n)
		}
	}
}
