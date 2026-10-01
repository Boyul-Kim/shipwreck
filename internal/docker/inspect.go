package docker

import (
	"context"
	"fmt"
	"net/url"
)

// Resources is the subset of HostConfig that POST /containers/{id}/update
// accepts. The same struct decodes the values GET /containers/{id}/json
// reports, so a snapshot and an update body are the same type.
//
// Every field is omitempty because the daemon treats an absent -- or zero --
// field as "leave this alone". That is also why restoring a limit that was
// never set is not always expressible; see Restore.
type Resources struct {
	Memory            int64  `json:"Memory,omitempty"`
	MemorySwap        int64  `json:"MemorySwap,omitempty"`
	MemoryReservation int64  `json:"MemoryReservation,omitempty"`
	NanoCpus          int64  `json:"NanoCpus,omitempty"`
	CpuPeriod         int64  `json:"CpuPeriod,omitempty"`
	CpuQuota          int64  `json:"CpuQuota,omitempty"`
	CpuShares         int64  `json:"CpuShares,omitempty"`
	PidsLimit         *int64 `json:"PidsLimit,omitempty"`
	BlkioWeight       uint16 `json:"BlkioWeight,omitempty"`
}

type State struct {
	Status     string `json:"Status"`
	Running    bool   `json:"Running"`
	Paused     bool   `json:"Paused"`
	Restarting bool   `json:"Restarting"`
	OOMKilled  bool   `json:"OOMKilled"`
	ExitCode   int    `json:"ExitCode"`
}

// EndpointSettings is one container's attachment to one network. It is kept so
// a reconnect can restore the aliases -- and a static address, when the
// endpoint had one -- instead of silently landing on a fresh IP.
type EndpointSettings struct {
	NetworkID  string      `json:"NetworkID"`
	EndpointID string      `json:"EndpointID"`
	IPAddress  string      `json:"IPAddress"`
	Gateway    string      `json:"Gateway"`
	Aliases    []string    `json:"Aliases"`
	Links      []string    `json:"Links"`
	IPAMConfig *IPAMConfig `json:"IPAMConfig"`
}

type IPAMConfig struct {
	IPv4Address  string   `json:"IPv4Address,omitempty"`
	IPv6Address  string   `json:"IPv6Address,omitempty"`
	LinkLocalIPs []string `json:"LinkLocalIPs,omitempty"`
}

type Inspected struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State State  `json:"State"`

	HostConfig struct {
		Resources
	} `json:"HostConfig"`

	NetworkSettings struct {
		Networks map[string]EndpointSettings `json:"Networks"`
	} `json:"NetworkSettings"`
}

func Inspect(ctx context.Context, id string) (Inspected, error) {
	var out Inspected

	path := "/containers/" + url.PathEscape(id) + "/json"
	if err := get(ctx, path, &out); err != nil {
		return Inspected{}, fmt.Errorf("error inspecting %s: %w", id, err)
	}

	return out, nil
}
