package docker

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

const (
	// Markers are carried in the command line so a fault can be found again
	// from inside the container: the burners to kill them, the scan to hide it
	// from its own listing, the hosts entry to remove it.
	burnMarker = "shipwreck-burn"
	scanMarker = "shipwreck-scan"
	hostMarker = "# shipwreck"

	// resolvBackup holds the original resolver config for the duration of a DNS
	// fault. /etc/resolv.conf is a per-container bind mount, so both the break
	// and the backup are invisible to every other container.
	resolvBackup = "/etc/resolv.conf.shipwreck"

	// DefaultFillPath is where a disk fill writes unless told otherwise.
	DefaultFillPath = "/tmp/shipwreck.fill"

	// BlackholeAddress is from TEST-NET-3, which is documentation space and
	// routed nowhere. A connection to it hangs until it times out, which is the
	// failure worth testing; 127.0.0.1 would be refused immediately instead.
	BlackholeAddress = "203.0.113.1"

	// BrokenNameserver is the same, for a resolver that never answers.
	BrokenNameserver = "203.0.113.1"
)

const processColumns = "PID\tCOMMAND"

type Process struct {
	PID int
	Cmd string
}

// Burner reports whether this process is a CPU burner shipwreck started.
func (p Process) Burner() bool {
	return strings.Contains(p.Cmd, burnMarker)
}

/*
*

	BurnCPU starts n spin loops in the container, each as its own detached exec.

	They are marked in their command line so StopBurn can find them again. They
	outlive the request that started them and keep running until they are killed
	or the container stops.

*
*/
func BurnCPU(ctx context.Context, id string, workers int) ([]string, error) {
	script := "while :; do :; done # " + burnMarker

	var started []string

	for range workers {
		execID, err := ShellDetached(ctx, id, script)
		if err != nil {
			return started, err
		}

		started = append(started, execID)
	}

	return started, nil
}

// StopBurn kills every burner shipwreck started in this container and reports
// how many it signalled.
func StopBurn(ctx context.Context, id string) (int, error) {
	processes, err := Processes(ctx, id)
	if err != nil {
		return 0, err
	}

	killed := 0

	for _, p := range processes {
		if !p.Burner() {
			continue
		}

		if _, err := KillProcess(ctx, id, p.PID, "KILL"); err != nil {
			return killed, err
		}

		killed++
	}

	return killed, nil
}

/*
*

	Processes lists the processes inside the container, as the container's own
	PID namespace numbers them.

	It reads /proc rather than calling ps, because a slim image often has no ps,
	and rather than GET /containers/{id}/top, which reports the host's PIDs --
	numbers that mean nothing to a kill run inside the container.

*
*/
func Processes(ctx context.Context, id string) ([]Process, error) {
	const script = `for d in /proc/[0-9]*; do
  p=${d#/proc/}
  [ -r "$d/cmdline" ] || continue
  c=$(tr '\0' ' ' < "$d/cmdline" 2>/dev/null)
  [ -n "$c" ] || c="[$(cat "$d/comm" 2>/dev/null)]"
  echo "$p $c"
done # ` + scanMarker

	result, err := Shell(ctx, id, script)
	if err != nil {
		return nil, err
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf("error listing processes in %s: exit %d: %s", short(id), result.ExitCode, result.Output())
	}

	return parseProcesses(result.Stdout), nil
}

// One "PID command" line per process, lowest PID first.
func parseProcesses(stdout string) []Process {
	var processes []Process

	for _, line := range strings.Split(stdout, "\n") {
		pid, cmd, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}

		n, err := strconv.Atoi(pid)
		if err != nil {
			continue
		}

		// The scan itself, and the shell that ran it, are not interesting.
		if strings.Contains(cmd, scanMarker) {
			continue
		}

		processes = append(processes, Process{PID: n, Cmd: strings.TrimSpace(cmd)})
	}

	sort.Slice(processes, func(i, j int) bool { return processes[i].PID < processes[j].PID })

	return processes
}

// KillProcess signals one process inside the container. Signalling a child
// rather than PID 1 is the point: the container stays up while one worker dies,
// which is the case a supervisor or a connection pool has to survive.
func KillProcess(ctx context.Context, id string, pid int, signal string) (ExecResult, error) {
	script := fmt.Sprintf("kill -s %s %d", quote(strings.TrimPrefix(signal, "SIG")), pid)

	return Shell(ctx, id, script)
}

// FillDisk writes megabytes of zeroes to path inside the container, filling
// whatever filesystem backs it -- the container's writable layer by default, or
// a volume if the path is on one.
func FillDisk(ctx context.Context, id string, path string, megabytes int) (ExecResult, error) {
	script := fmt.Sprintf("dd if=/dev/zero of=%s bs=1M count=%d", quote(path), megabytes)

	return Shell(ctx, id, script)
}

// RemoveFill deletes a fill file and reports what is left on the container's
// filesystems.
func RemoveFill(ctx context.Context, id string, path string) (ExecResult, error) {
	script := fmt.Sprintf(`rm -f %s
df -h 2>/dev/null || true`, quote(path))

	return Shell(ctx, id, script)
}

// BreakDNS points the container's resolver at an address that never answers, so
// every lookup hangs and then fails. The original file is kept once, so
// breaking twice does not back up the broken copy.
func BreakDNS(ctx context.Context, id string, nameserver string) (ExecResult, error) {
	script := fmt.Sprintf(`[ -f %[1]s ] || cp /etc/resolv.conf %[1]s
printf 'nameserver %%s\n' %[2]s > /etc/resolv.conf
cat /etc/resolv.conf`, quote(resolvBackup), quote(nameserver))

	return Shell(ctx, id, script)
}

func RestoreDNS(ctx context.Context, id string) (ExecResult, error) {
	script := fmt.Sprintf(`if [ ! -f %[1]s ]; then echo 'no shipwreck backup of /etc/resolv.conf' >&2; exit 1; fi
cat %[1]s > /etc/resolv.conf
rm -f %[1]s
cat /etc/resolv.conf`, quote(resolvBackup))

	return Shell(ctx, id, script)
}

// Blackhole sends one hostname to an address that routes nowhere, so the
// container's calls to that dependency hang instead of failing fast. /etc/hosts
// is a per-container bind mount, so only this container is affected.
func Blackhole(ctx context.Context, id string, host string, address string) (ExecResult, error) {
	script := fmt.Sprintf(`printf '%%s\t%%s\t%s\n' %s %s >> /etc/hosts
cat /etc/hosts`, hostMarker, quote(address), quote(host))

	return Shell(ctx, id, script)
}

// ClearBlackholes removes every entry shipwreck added to /etc/hosts. The file is
// rewritten in place because it is a bind mount and cannot be replaced.
func ClearBlackholes(ctx context.Context, id string) (ExecResult, error) {
	script := fmt.Sprintf(`grep -v %s /etc/hosts > /tmp/.shipwreck-hosts || true
cat /tmp/.shipwreck-hosts > /etc/hosts
rm -f /tmp/.shipwreck-hosts
cat /etc/hosts`, quote(hostMarker))

	return Shell(ctx, id, script)
}

func ProcessRows(processes []Process) (string, []string) {
	var buf bytes.Buffer

	w := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, processColumns)

	for _, p := range processes {
		cmd := p.Cmd

		switch {
		case p.PID == 1:
			cmd += "   <- init, signalling this one ends the container"

		case p.Burner():
			cmd += "   <- shipwreck cpu burner"
		}

		fmt.Fprintf(w, "%d\t%s\n", p.PID, cmd)
	}

	w.Flush()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

	return lines[0], lines[1:]
}

// Wraps a value for /bin/sh. Paths, hostnames and addresses come from the
// prompt, and they end up inside a script.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
