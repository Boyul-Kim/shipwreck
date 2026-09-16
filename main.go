package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"shipwreck/dial"
	"strings"
	"text/tabwriter"
	"time"
)

const apiVersion = "v1.41"

type container struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host := dial.DefaultHost()
	conn, err := dial.Dial(ctx, host)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", host, err)
	}

	defer conn.Close()

	containers, err := listContainers(ctx, conn)
	if err != nil {
		return err
	}

	return render(containers)
}

func listContainers(ctx context.Context, conn io.ReadWriteCloser) ([]container, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/"+apiVersion+"/containers/json?all=1", nil)

	if err != nil {
		return nil, err
	}

	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("writing request: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("docker api: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}

	var out []container
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return out, nil
}

func render(containers []container) error {
	if len(containers) == 0 {
		fmt.Println("no containers")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CONTAINER ID\tIMAGE\tSTATE\tNAME")
	for _, c := range containers {
		var name string
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, c.Image, c.State, name)
	}

	return w.Flush()
}
