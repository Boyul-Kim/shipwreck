package docker

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// cpuPeriod is the scheduler window a CPU squeeze is expressed against.
const cpuPeriod = 100000

// blkioDefault is the cgroup default weight, used when a container had no
// weight of its own to restore.
const blkioDefault = 500

// Originals for every container this process has squeezed. POST
// /containers/{id}/update overwrites the container's HostConfig, so after the
// first squeeze the pre-fault values exist nowhere else -- not in the daemon,
// and not across a restart of shipwreck. They are printed when recorded so
// they survive in the session's scrollback.
var snapshots = struct {
	mu   sync.Mutex
	byID map[string]Resources
}{byID: map[string]Resources{}}

// Update sends one resource change and returns any warning the daemon reports
// (an unsupported knob is a warning, not an error, so it has to be surfaced).
func Update(ctx context.Context, id string, r Resources) ([]string, error) {
	var out struct {
		Warnings []string `json:"Warnings"`
	}

	path := "/containers/" + url.PathEscape(id) + "/update"
	if err := post(ctx, path, r, &out); err != nil {
		return nil, fmt.Errorf("error updating %s: %w", id, err)
	}

	return out.Warnings, nil
}

// Snapshot records the container's current limits the first time it is called
// for that container and returns whatever is on record. Later calls keep the
// first reading, so squeezing twice does not overwrite the originals with
// already-squeezed values.
func Snapshot(ctx context.Context, id string) (Resources, bool, error) {
	if r, ok := Recorded(id); ok {
		return r, false, nil
	}

	inspected, err := Inspect(ctx, id)
	if err != nil {
		return Resources{}, false, err
	}

	snapshots.mu.Lock()
	defer snapshots.mu.Unlock()

	// Another caller may have recorded it while the inspect was in flight.
	if r, ok := snapshots.byID[id]; ok {
		return r, false, nil
	}

	snapshots.byID[id] = inspected.HostConfig.Resources

	return inspected.HostConfig.Resources, true, nil
}

func Recorded(id string) (Resources, bool) {
	snapshots.mu.Lock()
	defer snapshots.mu.Unlock()

	r, ok := snapshots.byID[id]

	return r, ok
}

// SqueezeMemory lowers the memory limit to bytes and pins the swap limit to the
// same value, so the container has nowhere to spill and the kernel OOM killer
// runs instead.
func SqueezeMemory(ctx context.Context, id string, limit int64) ([]string, error) {
	if _, _, err := Snapshot(ctx, id); err != nil {
		return nil, err
	}

	return Update(ctx, id, Resources{Memory: limit, MemorySwap: limit})
}

/*
*

	SqueezeCPU caps the container at the given number of CPUs.

	Which knob it uses depends on the snapshot, because only one of them can be
	undone. A container that started with no CPU limit is squeezed through
	CpuQuota, since a quota of -1 means "unlimited" and so restores cleanly,
	where a zero would only mean "unchanged". A container that already had
	NanoCpus is squeezed through NanoCpus, which takes precedence over quota in
	the daemon and would otherwise mask the restore.

*
*/
func SqueezeCPU(ctx context.Context, id string, cpus float64) ([]string, error) {
	original, _, err := Snapshot(ctx, id)
	if err != nil {
		return nil, err
	}

	if original.NanoCpus != 0 {
		return Update(ctx, id, Resources{NanoCpus: int64(cpus * 1e9)})
	}

	period := original.CpuPeriod
	if period == 0 {
		period = cpuPeriod
	}

	quota := int64(cpus * float64(period))

	// The daemon rejects a quota under 1ms.
	if quota < 1000 {
		quota = 1000
	}

	return Update(ctx, id, Resources{CpuPeriod: period, CpuQuota: quota})
}

// SqueezePids lowers the process limit, so the next fork or thread creation in
// the container fails.
func SqueezePids(ctx context.Context, id string, limit int64) ([]string, error) {
	if _, _, err := Snapshot(ctx, id); err != nil {
		return nil, err
	}

	return Update(ctx, id, Resources{PidsLimit: &limit})
}

// SqueezeBlkio lowers the container's share of disk bandwidth. Weight runs from
// 10 to 1000 and needs a daemon with blkio weight support -- without it the
// daemon answers with a warning and changes nothing.
func SqueezeBlkio(ctx context.Context, id string, weight uint16) ([]string, error) {
	if _, _, err := Snapshot(ctx, id); err != nil {
		return nil, err
	}

	return Update(ctx, id, Resources{BlkioWeight: weight})
}

/*
*

	Restore puts back the limits recorded by the first Snapshot for this
	container and forgets them.

	Not every original is expressible. The update API reads an absent or zero
	field as "leave alone", so "no limit at all" has no encoding for memory: the
	daemon rejects anything below 6MB, including -1. CPU and pids do have one
	(-1), so those come back exactly. Whatever cannot be restored is returned as
	a warning rather than swallowed.

*
*/
func Restore(ctx context.Context, id string) (Resources, []string, error) {
	original, ok := Recorded(id)
	if !ok {
		return Resources{}, nil, fmt.Errorf("no recorded limits for %s: nothing was squeezed in this session", id)
	}

	restore, warnings := restoreTarget(original)

	daemonWarnings, err := Update(ctx, id, restore)
	if err != nil {
		return restore, warnings, err
	}

	snapshots.mu.Lock()
	delete(snapshots.byID, id)
	snapshots.mu.Unlock()

	return restore, append(warnings, daemonWarnings...), nil
}

// The update body that puts original back, plus whatever it could not express.
func restoreTarget(original Resources) (Resources, []string) {
	restore := Resources{
		MemoryReservation: original.MemoryReservation,
		CpuShares:         original.CpuShares,
	}

	var warnings []string

	if original.Memory != 0 {
		// Memory and swap go in one request: the swap limit still holds the
		// squeezed value, and the kernel refuses a memory limit above it.
		restore.Memory = original.Memory
		restore.MemorySwap = original.MemorySwap

		// An unset swap limit alongside a memory limit means twice the memory,
		// which is the daemon's own default.
		if restore.MemorySwap == 0 {
			restore.MemorySwap = original.Memory * 2
		}
	} else {
		warnings = append(warnings, "memory had no limit originally, and the update API cannot express that (its floor is 6MB); recreate the container to clear the limit")
	}

	switch {
	case original.NanoCpus != 0:
		restore.NanoCpus = original.NanoCpus

	case original.CpuQuota != 0:
		restore.CpuQuota = original.CpuQuota
		restore.CpuPeriod = original.CpuPeriod

	default:
		restore.CpuQuota = -1
	}

	if original.PidsLimit != nil && *original.PidsLimit > 0 {
		restore.PidsLimit = original.PidsLimit
	} else {
		unlimited := int64(-1)
		restore.PidsLimit = &unlimited
	}

	if original.BlkioWeight != 0 {
		restore.BlkioWeight = original.BlkioWeight
	} else {
		restore.BlkioWeight = blkioDefault
		warnings = append(warnings, fmt.Sprintf("blkio weight was unset originally; putting it back to the cgroup default of %d", blkioDefault))
	}

	return restore, warnings
}

// FormatResources renders the limits a human cares about, using "unlimited"
// for the zero values the API reports for an absent limit.
func FormatResources(r Resources) string {
	var b strings.Builder

	fmt.Fprintf(&b, "  memory       %s\n", formatBytes(r.Memory))
	fmt.Fprintf(&b, "  memory+swap  %s\n", formatBytes(r.MemorySwap))

	switch {
	case r.NanoCpus != 0:
		fmt.Fprintf(&b, "  cpus         %.2f (NanoCpus)\n", float64(r.NanoCpus)/1e9)

	case r.CpuQuota > 0:
		period := r.CpuPeriod
		if period == 0 {
			period = cpuPeriod
		}

		fmt.Fprintf(&b, "  cpus         %.2f (quota %d / period %d)\n", float64(r.CpuQuota)/float64(period), r.CpuQuota, period)

	default:
		fmt.Fprintln(&b, "  cpus         unlimited")
	}

	if r.PidsLimit != nil && *r.PidsLimit > 0 {
		fmt.Fprintf(&b, "  pids         %d\n", *r.PidsLimit)
	} else {
		fmt.Fprintln(&b, "  pids         unlimited")
	}

	if r.BlkioWeight != 0 {
		fmt.Fprintf(&b, "  blkio        %d\n", r.BlkioWeight)
	} else {
		fmt.Fprintln(&b, "  blkio        unset")
	}

	return b.String()
}

func formatBytes(n int64) string {
	if n == 0 {
		return "unlimited"
	}

	if n < 0 {
		return "unlimited (-1)"
	}

	const mb = 1 << 20
	if n >= mb {
		return fmt.Sprintf("%.0fMB (%d)", float64(n)/mb, n)
	}

	return fmt.Sprintf("%d bytes", n)
}
