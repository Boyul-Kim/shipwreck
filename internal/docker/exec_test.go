package docker

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// Builds one frame of Docker's multiplexed exec stream.
func frame(stream byte, payload string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))

	return append(header, payload...)
}

func TestDemuxSplitsStreams(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(frame(1, "out one\n"))
	stream.Write(frame(2, "err one\n"))
	stream.Write(frame(1, "out two\n"))

	stdout, stderr, err := demux(&stream)
	if err != nil {
		t.Fatalf("demux: %v", err)
	}

	if want := "out one\nout two\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	if want := "err one\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

func TestDemuxHandlesEmptyAndLargeFrames(t *testing.T) {
	big := strings.Repeat("a", frameChunk+7)

	var stream bytes.Buffer
	stream.Write(frame(1, ""))
	stream.Write(frame(1, big))

	stdout, _, err := demux(&stream)
	if err != nil {
		t.Fatalf("demux: %v", err)
	}

	if stdout != big {
		t.Errorf("stdout length = %d, want %d", len(stdout), len(big))
	}
}

// A container that dies mid-frame closes the stream early. That is the end of
// the output, not an error the caller has to handle.
func TestDemuxStopsAtTruncatedFrame(t *testing.T) {
	var stream bytes.Buffer
	stream.Write(frame(1, "kept\n"))
	stream.Write(frame(1, "lost")[:10])

	stdout, _, err := demux(&stream)
	if err != nil {
		t.Fatalf("demux: %v", err)
	}

	if !strings.HasPrefix(stdout, "kept\n") {
		t.Errorf("stdout = %q, want it to start with the complete frame", stdout)
	}
}

func TestDemuxCapsCapturedOutputButDrainsStream(t *testing.T) {
	var stream bytes.Buffer
	for range (captureLimit / frameChunk) + 4 {
		stream.Write(frame(1, strings.Repeat("x", frameChunk)))
	}

	stdout, _, err := demux(&stream)
	if err != nil {
		t.Fatalf("demux: %v", err)
	}

	if len(stdout) > captureLimit+frameChunk {
		t.Errorf("captured %d bytes, want no more than %d", len(stdout), captureLimit+frameChunk)
	}

	if stream.Len() != 0 {
		t.Errorf("%d bytes left unread; the stream has to be drained", stream.Len())
	}
}

func TestExecResultOutput(t *testing.T) {
	tests := []struct {
		name   string
		result ExecResult
		want   string
	}{
		{name: "stdout only", result: ExecResult{Stdout: "out\n"}, want: "out"},
		{name: "stderr only", result: ExecResult{Stderr: "boom\n"}, want: "boom"},
		{name: "both", result: ExecResult{Stdout: "out\n", Stderr: "boom\n"}, want: "out\nboom"},
		{name: "neither", result: ExecResult{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Output(); got != tt.want {
				t.Errorf("Output() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestQuoteSurvivesASingleQuote(t *testing.T) {
	got := quote("/tmp/it's here")

	if want := `'/tmp/it'\''s here'`; got != want {
		t.Errorf("quote() = %s, want %s", got, want)
	}
}
