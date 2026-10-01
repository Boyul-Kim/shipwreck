package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

const columns = "CONTAINER ID	IMAGE	STATE	NAME"

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
	return kill(ctx, id, "SIGKILL")
}

func Sigterm(ctx context.Context, id string) error {
	return kill(ctx, id, "SIGTERM")
}

func kill(ctx context.Context, id string, signal string) error {
	path := "/containers/" + url.PathEscape(id) + "/kill?signal=" + signal

	if err := post(ctx, path, nil, nil); err != nil {
		return fmt.Errorf("error sending %s to %s: %w", signal, id, err)
	}

	return nil
}

func Stop(ctx context.Context, id string, timeout int) error {
	path := fmt.Sprintf("/containers/%s/stop?t=%d", url.PathEscape(id), timeout)

	if err := post(ctx, path, nil, nil); err != nil {
		return fmt.Errorf("error stopping %s: %w", id, err)
	}

	return nil
}

func Restart(ctx context.Context, id string, timeout int) error {
	path := fmt.Sprintf("/containers/%s/restart?t=%d", url.PathEscape(id), timeout)

	if err := post(ctx, path, nil, nil); err != nil {
		return fmt.Errorf("error restarting %s: %w", id, err)
	}

	return nil
}

// Pause suspends every process in the container with the cgroup freezer. Open
// TCP connections survive, so a client sees a peer that accepts connections
// and then never answers -- the hung dependency case.
func Pause(ctx context.Context, id string) error {
	path := "/containers/" + url.PathEscape(id) + "/pause"

	if err := post(ctx, path, nil, nil); err != nil {
		return fmt.Errorf("error pausing %s: %w", id, err)
	}

	return nil
}

func Unpause(ctx context.Context, id string) error {
	path := "/containers/" + url.PathEscape(id) + "/unpause"

	if err := post(ctx, path, nil, nil); err != nil {
		return fmt.Errorf("error unpausing %s: %w", id, err)
	}

	return nil
}

func Render(containers []Container) error {
	if len(containers) == 0 {
		fmt.Println("no containers")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, columns)
	writeRows(w, containers)

	return w.Flush()
}

func Rows(containers []Container) (string, []string) {
	var buf bytes.Buffer

	w := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, columns)
	writeRows(w, containers)
	w.Flush()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	return lines[0], lines[1:]
}

func writeRows(w io.Writer, containers []Container) {
	for _, c := range containers {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.ShortID(), c.ShortImage(), c.State, c.Name())
	}
}
