package docker

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"text/tabwriter"
)

const networkColumns = "NETWORK\tDRIVER\tSCOPE\tIPV4"

type Network struct {
	ID         string `json:"Id"`
	Name       string `json:"Name"`
	Driver     string `json:"Driver"`
	Scope      string `json:"Scope"`
	Internal   bool   `json:"Internal"`
	Attachable bool   `json:"Attachable"`
}

// Endpoints a partition removed, keyed by container ID and network name. A
// disconnect throws away the container's aliases and address, and the daemon
// hands out a fresh address on reconnect, so they are kept here to be put back.
var endpoints = struct {
	mu   sync.Mutex
	byID map[string]EndpointSettings
}{byID: map[string]EndpointSettings{}}

func Networks(ctx context.Context) ([]Network, error) {
	var out []Network
	if err := get(ctx, "/networks", &out); err != nil {
		return nil, fmt.Errorf("error listing networks: %w", err)
	}

	return out, nil
}

// Attached reports the networks the container is currently on, as a name to
// endpoint map, alongside the daemon's view of each of those networks. host and
// none are left out: they are namespace modes, and there is no endpoint to cut.
func Attached(ctx context.Context, id string) (map[string]EndpointSettings, []Network, error) {
	inspected, err := Inspect(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	joined := inspected.NetworkSettings.Networks

	all, err := Networks(ctx)
	if err != nil {
		return nil, nil, err
	}

	known := map[string]bool{}

	var on []Network

	for _, n := range all {
		if _, ok := joined[n.Name]; !ok || namespaceMode(n.Name) {
			continue
		}

		known[n.Name] = true
		on = append(on, n)
	}

	// An endpoint whose network the list did not report -- a swarm scoped one,
	// say -- is still attached, and the endpoint carries the ID a disconnect
	// needs.
	for name, endpoint := range joined {
		if known[name] || namespaceMode(name) || endpoint.NetworkID == "" {
			continue
		}

		on = append(on, Network{ID: endpoint.NetworkID, Name: name, Driver: "?", Scope: "?"})
	}

	return joined, on, nil
}

// host and none are namespace modes rather than networks with endpoints, and
// swarm's ingress rejects a plain container.
func namespaceMode(name string) bool {
	return name == "host" || name == "none" || name == "ingress"
}

/*
*

	Joinable reports the networks the container could be put on: every network it
	is not currently attached to, minus the ones a container cannot join through
	the connect endpoint.

	Networks it was cut off from sort first, since the usual next step after a
	partition is to undo it.

*
*/
func Joinable(ctx context.Context, id string) ([]Network, error) {
	inspected, err := Inspect(ctx, id)
	if err != nil {
		return nil, err
	}

	all, err := Networks(ctx)
	if err != nil {
		return nil, err
	}

	var remembered, rest []Network

	for _, n := range all {
		if _, ok := inspected.NetworkSettings.Networks[n.Name]; ok {
			continue
		}

		if namespaceMode(n.Name) {
			continue
		}

		// A swarm network that was not created attachable refuses a plain
		// container, so offering it would only produce an error.
		if n.Scope == "swarm" && !n.Attachable {
			continue
		}

		if _, ok := rememberedEndpoint(id, n.Name); ok {
			remembered = append(remembered, n)
			continue
		}

		rest = append(rest, n)
	}

	return append(remembered, rest...), nil
}

/*
*

	Partition disconnects the container from one network with Force, which is
	what makes it work on a running container: without it the daemon refuses to
	tear down a live endpoint.

	The other networks are untouched, so isolating a service from the database
	network while it stays reachable from the API network gives a partial
	partition rather than an outage.

*
*/
func Partition(ctx context.Context, containerID string, network Network) error {
	inspected, err := Inspect(ctx, containerID)
	if err != nil {
		return err
	}

	endpoint, ok := inspected.NetworkSettings.Networks[network.Name]
	if !ok {
		return fmt.Errorf("%s is not attached to %s", short(containerID), network.Name)
	}

	body := struct {
		Container string
		Force     bool
	}{Container: containerID, Force: true}

	path := "/networks/" + url.PathEscape(network.ID) + "/disconnect"
	if err := post(ctx, path, body, nil); err != nil {
		return fmt.Errorf("error disconnecting %s from %s: %w", short(containerID), network.Name, err)
	}

	remember(containerID, network.Name, endpoint)

	return nil
}

/*
*

	Rejoin connects the container back to a network, restoring the aliases the
	disconnect dropped so service discovery works under the same name as before.

	The address is only restored when the endpoint had a static one: asking for a
	previously dynamic address risks a clash with whatever the daemon handed out
	in the meantime, and the default bridge rejects a requested address outright.

*
*/
func Rejoin(ctx context.Context, containerID string, network Network) (EndpointSettings, bool, error) {
	endpoint, restored := rememberedEndpoint(containerID, network.Name)

	config := struct {
		Aliases    []string    `json:"Aliases,omitempty"`
		IPAMConfig *IPAMConfig `json:"IPAMConfig,omitempty"`
	}{}

	if restored {
		config.Aliases = endpoint.Aliases

		if endpoint.IPAMConfig != nil && endpoint.IPAMConfig.IPv4Address != "" {
			config.IPAMConfig = endpoint.IPAMConfig
		}
	}

	body := struct {
		Container      string
		EndpointConfig any
	}{Container: containerID, EndpointConfig: config}

	path := "/networks/" + url.PathEscape(network.ID) + "/connect"
	if err := post(ctx, path, body, nil); err != nil {
		return endpoint, restored, fmt.Errorf("error connecting %s to %s: %w", short(containerID), network.Name, err)
	}

	forget(containerID, network.Name)

	return endpoint, restored, nil
}

func NetworkRows(networks []Network, attached map[string]EndpointSettings) (string, []string) {
	var buf bytes.Buffer

	w := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, networkColumns)

	for _, n := range networks {
		address := "-"
		if e, ok := attached[n.Name]; ok && e.IPAddress != "" {
			address = e.IPAddress
		}

		scope := n.Scope
		if n.Internal {
			scope += ",internal"
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", n.Name, n.Driver, scope, address)
	}

	w.Flush()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	return lines[0], lines[1:]
}

func remember(containerID string, network string, e EndpointSettings) {
	endpoints.mu.Lock()
	defer endpoints.mu.Unlock()

	endpoints.byID[containerID+"|"+network] = e
}

func rememberedEndpoint(containerID string, network string) (EndpointSettings, bool) {
	endpoints.mu.Lock()
	defer endpoints.mu.Unlock()

	e, ok := endpoints.byID[containerID+"|"+network]

	return e, ok
}

func forget(containerID string, network string) {
	endpoints.mu.Lock()
	defer endpoints.mu.Unlock()

	delete(endpoints.byID, containerID+"|"+network)
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}

	return id
}
