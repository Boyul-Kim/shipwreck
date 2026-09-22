package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"shipwreck/internal/dial"
	"strings"
	"text/tabwriter"
	"time"
)

const apiVersion = "v1.41"

type Container struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
}

func (c Container) Name() string {
	if len(c.Names) == 0 {
		return ""
	}

	return strings.TrimPrefix(c.Names[0], "/")
}

func (c Container) ShortID() string {
	if len(c.ID) > 12 {
		return c.ID[:12]
	}

	return c.ID
}

func (c Container) ShortImage() string {
	if id, ok := strings.CutPrefix(c.Image, "sha256:"); ok && len(id) > 12 {
		return "sha256:" + id[:12]
	}

	return c.Image
}

func Run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	containers, err := List(ctx)
	if err != nil {
		return err
	}

	return Render(containers)
}

func List(ctx context.Context) ([]Container, error) {
	var out []Container
	if err := get(ctx, "/containers/json?all=1", &out); err != nil {
		return nil, err
	}

	return out, nil
}

func Sigkill(ctx context.Context, id string) error {
	path := "/containers/" + url.PathEscape(id) + "/kill?signal=SIGKILL"

	if err := post(ctx, path, nil); err != nil {
		return fmt.Errorf("error killing %s: %w", id, err)
	}

	return nil
}

func do(ctx context.Context, method string, path string, v any) error {
	host := dial.DefaultHost()
	conn, err := dial.Dial(ctx, host)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", host, err)
	}

	defer conn.Close()

	req, err := http.NewRequestWithContext(ctx, method, "http://docker/"+apiVersion+path, nil)
	if err != nil {
		return err
	}

	if err := req.Write(conn); err != nil {
		return fmt.Errorf("writing request: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	defer resp.Body.Close()

	// A status like kill's 204 wouldn't pass a plain StatusOK check.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("docker api: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}

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
	return do(ctx, http.MethodGet, path, v)
}

func post(ctx context.Context, path string, v any) error {
	return do(ctx, http.MethodPost, path, v)
}

func Render(containers []Container) error {
	if len(containers) == 0 {
		fmt.Println("no containers")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CONTAINER ID\tIMAGE\tSTATE\tNAME")
	for _, c := range containers {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.ShortID(), c.ShortImage(), c.State, c.Name())
	}

	return w.Flush()
}
