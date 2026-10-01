package menu

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"shipwreck/internal/docker"
	"shipwreck/internal/term"
	"strconv"
	"strings"
	"time"
)

type MenuOption[A int, B string] struct {
	Number A
	Value  B
}

// action is one leaf of a submenu: a label and the thing it does.
type action struct {
	Label string
	Run   func(*bufio.Reader)
}

const timeout = 10 * time.Second

// execTimeout is longer than timeout because a disk fill writes for as long as
// it takes, and the daemon holds the exec stream open until it is done.
const execTimeout = 5 * time.Minute

const defaultDrainTimeout = 10

const backHint = "up/down to move, Enter to select, b or q to go back"

func Menu() {
	fmt.Print(banner)

	reader := bufio.NewReader(os.Stdin)
	choices := loadMenuChoices()

	const hint = "up/down to move, Enter to select, q to abandon ship"

	for {
		picked, err := term.Select(reader, "\n"+title+"\n"+hint, choices)
		if errors.Is(err, term.ErrBack) {
			continue
		}

		if errors.Is(err, term.ErrCancelled) {
			abandonShip()
			return
		}

		if err != nil {
			fmt.Fprintln(os.Stderr, "shipwreck:", err)
			continue
		}

		switch picked {
		case 1:
			listContainers()
		case 2:
			sigkillContainer(reader)
		case 3:
			sigtermContainer(reader)
		case 4:
			stopContainer(reader)
		case 5:
			restartContainer(reader)
		case 6:
			freezeMenu(reader)
		case 7:
			partitionMenu(reader)
		case 8:
			squeezeMenu(reader)
		case 9:
			faultMenu(reader)
		case 10:
			abandonShip()
			return
		}
	}
}

func listContainers() {
	fmt.Print("\nFetching containers...\n\n")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	containers, err := docker.List(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if err := docker.Render(containers); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
	}
}

func sigkillContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to sigkill, b or q to go back"

	picked, ok := pickContainer(in, hint)
	if !ok {
		return
	}

	fmt.Printf("\nSigkill for %s...\n", describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := docker.Sigkill(ctx, picked.ID); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nSigkill successful\n")
}

func sigtermContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to send SIGTERM, b or q to go back"

	picked, ok := pickContainer(in, hint)
	if !ok {
		return
	}

	fmt.Printf("\nSIGTERM for %s...\n", describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := docker.Sigterm(ctx, picked.ID); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nSIGTERM sent\n")
}

func stopContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to stop, b or q to go back"

	picked, ok := pickContainer(in, hint)
	if !ok {
		return
	}

	t, err := promptInt(in, "Drain timeout in seconds", defaultDrainTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Printf("\nStopping %s (draining up to %ds)...\n", describe(picked), t)

	ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Duration(t)*time.Second)
	defer cancel()

	if err := docker.Stop(ctx, picked.ID, t); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nStop successful\n")
}

func restartContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to restart, b or q to go back"

	picked, ok := pickContainer(in, hint)
	if !ok {
		return
	}

	fmt.Printf("\nRestarting %s...\n", describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout+defaultDrainTimeout*time.Second)
	defer cancel()

	if err := docker.Restart(ctx, picked.ID, defaultDrainTimeout); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nRestart successful\n")
}

// ---------------------------------------------------------------- freeze

func freezeMenu(in *bufio.Reader) {
	submenu(in, "Freeze -- suspend a container without closing its sockets", []action{
		{Label: "Pause Container (freeze processes, keep connections open)", Run: pauseContainer},
		{Label: "Unpause Container", Run: unpauseContainer},
	})
}

func pauseContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to freeze, b or q to go back"

	picked, ok := pickRunning(in, hint)
	if !ok {
		return
	}

	fmt.Printf("\nFreezing %s...\n", describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := docker.Pause(ctx, picked.ID); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nFrozen. Its processes are suspended by the cgroup freezer, but\n")
	fmt.Print("its listening sockets and open connections are still there: a client\n")
	fmt.Print("connects, sends, and waits forever. Any request without a timeout\n")
	fmt.Print("will hang here until you unpause.\n")
}

func unpauseContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to thaw, b or q to go back"

	picked, ok := pickContainerWhere(in, hint, paused, "no paused containers")
	if !ok {
		return
	}

	fmt.Printf("\nThawing %s...\n", describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := docker.Unpause(ctx, picked.ID); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nThawed. Whatever was queued against it is delivered now.\n")
}

// ---------------------------------------------------------------- partition

func partitionMenu(in *bufio.Reader) {
	submenu(in, "Partition -- cut one container off one network at a time", []action{
		{Label: "Disconnect from a Network (partial partition)", Run: disconnectNetwork},
		{Label: "Reconnect to a Network (heal)", Run: connectNetwork},
		{Label: "Show a Container's Networks", Run: showNetworks},
	})
}

func disconnectNetwork(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	attached, networks, err := docker.Attached(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if len(networks) == 0 {
		fmt.Printf("\n%s is not on any network\n", describe(picked))
		return
	}

	network, ok := pickNetwork(in, "up/down to move, Enter to cut, b or q to go back", networks, attached)
	if !ok {
		return
	}

	fmt.Printf("\nDisconnecting %s from %s...\n", describe(picked), network.Name)

	cutCtx, cancelCut := context.WithTimeout(context.Background(), timeout)
	defer cancelCut()

	if err := docker.Partition(cutCtx, picked.ID, network); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Printf("\nCut. %s can no longer reach anything on %s, and its other\n", describe(picked), network.Name)
	fmt.Print("networks are untouched -- so it stays reachable from them while it\n")
	fmt.Print("cannot see this one. Its aliases were recorded; reconnect restores them.\n")
}

func connectNetwork(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	networks, err := docker.Joinable(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if len(networks) == 0 {
		fmt.Printf("\n%s is already on every network it could join\n", describe(picked))
		return
	}

	network, ok := pickNetwork(in, "up/down to move, Enter to join, b or q to go back", networks, nil)
	if !ok {
		return
	}

	fmt.Printf("\nConnecting %s to %s...\n", describe(picked), network.Name)

	joinCtx, cancelJoin := context.WithTimeout(context.Background(), timeout)
	defer cancelJoin()

	endpoint, restored, err := docker.Rejoin(joinCtx, picked.ID, network)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nHealed.\n")

	if restored {
		if len(endpoint.Aliases) > 0 {
			fmt.Printf("Restored aliases: %s\n", strings.Join(endpoint.Aliases, ", "))
		}

		if endpoint.IPAddress != "" {
			fmt.Printf("Its address before the cut was %s; unless it was static the\n", endpoint.IPAddress)
			fmt.Print("daemon has handed out a fresh one.\n")
		}
	}
}

func showNetworks(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	attached, networks, err := docker.Attached(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if len(networks) == 0 {
		fmt.Printf("\n%s is not on any network\n", describe(picked))
		return
	}

	columns, rows := docker.NetworkRows(networks, attached)

	fmt.Printf("\n%s\n", columns)
	for _, r := range rows {
		fmt.Println(r)
	}
}

// ---------------------------------------------------------------- squeeze

func squeezeMenu(in *bufio.Reader) {
	submenu(in, "Squeeze -- take away memory, CPU, PIDs or disk bandwidth", []action{
		{Label: "Memory Limit (provoke the OOM killer)", Run: squeezeMemory},
		{Label: "CPU Limit (noisy neighbour)", Run: squeezeCPU},
		{Label: "PID Limit (make fork fail)", Run: squeezePids},
		{Label: "Blkio Weight (starve disk I/O)", Run: squeezeBlkio},
		{Label: "Show Recorded Limits", Run: showLimits},
		{Label: "Restore Limits", Run: restoreLimits},
	})
}

func squeezeMemory(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	mb, err := promptInt(in, "Memory limit in MB", 32)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if mb < 6 {
		fmt.Print("\nThe daemon's floor is 6MB; nothing sent.\n")
		return
	}

	if !record(picked) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	warnings, err := docker.SqueezeMemory(ctx, picked.ID, int64(mb)<<20)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showWarnings(warnings)

	fmt.Printf("\n%s is now capped at %dMB with swap pinned to the same value, so\n", describe(picked), mb)
	fmt.Print("there is nowhere to spill: the next allocation past the cap gets the\n")
	fmt.Print("OOM killer. Watch whether your process dies quietly or your health\n")
	fmt.Print("check keeps answering 200 for a container that has lost its worker.\n")
}

func squeezeCPU(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	cpus, err := promptFloat(in, "CPU limit in cores (0.1 = a tenth of one core)", 0.1)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if cpus <= 0 {
		fmt.Print("\nA CPU limit has to be greater than zero; nothing sent.\n")
		return
	}

	if !record(picked) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	warnings, err := docker.SqueezeCPU(ctx, picked.ID, cpus)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showWarnings(warnings)

	fmt.Printf("\n%s is now throttled to %.2f cores. Everything it does still\n", describe(picked), cpus)
	fmt.Print("works, only slowly -- which is the case that fills connection pools\n")
	fmt.Print("and trips deadlines that never fire when a dependency is simply down.\n")
}

func squeezePids(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	limit, err := promptInt(in, "PID limit", 20)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if limit < 1 {
		fmt.Print("\nA PID limit has to be at least 1; nothing sent.\n")
		return
	}

	if !record(picked) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	warnings, err := docker.SqueezePids(ctx, picked.ID, int64(limit))
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showWarnings(warnings)

	fmt.Printf("\n%s can hold %d processes and threads. Past that, fork and thread\n", describe(picked), limit)
	fmt.Print("creation fail -- the error path a runtime almost never exercises.\n")
}

func squeezeBlkio(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	weight, err := promptInt(in, "Blkio weight (10 lowest, 1000 highest)", 10)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if weight < 10 || weight > 1000 {
		fmt.Print("\nWeight has to be between 10 and 1000; nothing sent.\n")
		return
	}

	if !record(picked) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	warnings, err := docker.SqueezeBlkio(ctx, picked.ID, uint16(weight))
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showWarnings(warnings)

	fmt.Printf("\n%s now has a blkio weight of %d. It only bites while something\n", describe(picked), weight)
	fmt.Print("else is competing for the same disk -- and not at all on a daemon\n")
	fmt.Print("without blkio weight support, which reports a warning above.\n")
}

func showLimits(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	inspected, err := docker.Inspect(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Printf("\n%s is %s\n", describe(picked), describeState(inspected.State))
	fmt.Printf("\nLive limits:\n\n%s", docker.FormatResources(inspected.HostConfig.Resources))

	original, ok := docker.Recorded(picked.ID)
	if !ok {
		fmt.Print("\nNothing recorded: this container has not been squeezed in this session.\n")
		return
	}

	fmt.Printf("\nRecorded originals:\n\n%s", docker.FormatResources(original))
}

func restoreLimits(in *bufio.Reader) {
	picked, ok := pickContainer(in, backHint)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	restored, warnings, err := docker.Restore(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Printf("\nPut back on %s:\n\n%s", describe(picked), docker.FormatResources(restored))

	showWarnings(warnings)
}

// Records the pre-fault limits before the first squeeze of a container and
// prints them, so the originals are in the scrollback even though they only
// live in memory.
func record(c docker.Container) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	original, recorded, err := docker.Snapshot(ctx, c.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return false
	}

	if recorded {
		fmt.Printf("\nRecorded original limits for %s:\n\n%s", describe(c), docker.FormatResources(original))
	}

	return true
}

// ---------------------------------------------------------------- in-container

func faultMenu(in *bufio.Reader) {
	submenu(in, "In-container faults -- run through the exec API, needs /bin/sh", []action{
		{Label: "Burn CPU (spin loops inside the container)", Run: burnCPU},
		{Label: "Stop CPU Burn", Run: stopBurn},
		{Label: "Fill Disk", Run: fillDisk},
		{Label: "Remove Fill File", Run: removeFill},
		{Label: "Kill a Process (not PID 1)", Run: killProcess},
		{Label: "Break DNS", Run: breakDNS},
		{Label: "Restore DNS", Run: restoreDNS},
		{Label: "Blackhole a Hostname", Run: blackhole},
		{Label: "Clear Blackholes", Run: clearBlackholes},
	})
}

func burnCPU(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	workers, err := promptInt(in, "Spin loops to start", 1)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if workers < 1 {
		fmt.Print("\nNothing to start.\n")
		return
	}

	fmt.Printf("\nStarting %d spin loop(s) in %s...\n", workers, describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	started, err := docker.BurnCPU(ctx, picked.ID, workers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "shipwreck: started %d of %d: %v\n", len(started), workers, err)
		return
	}

	fmt.Printf("\n%d burner(s) running. They are detached, so they outlive this\n", len(started))
	fmt.Print("command and keep burning until you stop them or the container dies.\n")
	fmt.Print("Pair this with a CPU limit to make the squeeze visible immediately.\n")
}

func stopBurn(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	killed, err := docker.StopBurn(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if killed == 0 {
		fmt.Printf("\nNo shipwreck burners found in %s\n", describe(picked))
		return
	}

	fmt.Printf("\nKilled %d burner(s) in %s\n", killed, describe(picked))
}

func fillDisk(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	path, err := promptText(in, "Path to fill", docker.DefaultFillPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	mb, err := promptInt(in, "Megabytes to write", 512)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if mb < 1 {
		fmt.Print("\nNothing to write.\n")
		return
	}

	fmt.Printf("\nWriting %dMB to %s in %s...\n", mb, path, describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	result, err := docker.FillDisk(ctx, picked.ID, path, mb)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showExec(result)

	if result.ExitCode == 0 {
		fmt.Print("\nThe write went through. Whether that hurts depends on what else\n")
		fmt.Print("lives on this filesystem -- a log directory, a SQLite file, the\n")
		fmt.Print("writable layer -- and on whether your code checks its write errors.\n")
		return
	}

	fmt.Print("\ndd stopped early, which usually means the filesystem is already full.\n")
}

func removeFill(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	path, err := promptText(in, "Fill file to remove", docker.DefaultFillPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	result, err := docker.RemoveFill(ctx, picked.ID, path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showExec(result)
}

func killProcess(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	fmt.Print("\nListing processes...\n")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	processes, err := docker.Processes(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if len(processes) == 0 {
		fmt.Print("\nno processes\n")
		return
	}

	columns, rows := docker.ProcessRows(processes)

	choices := make([]term.Choice[docker.Process], 0, len(processes))
	for i, p := range processes {
		choices = append(choices, term.Choice[docker.Process]{Label: rows[i], Value: p})
	}

	header := "\n" + title + "\nup/down to move, Enter to signal, b or q to go back\n\n   " + columns

	process, err := term.Select(in, header, choices)
	if errors.Is(err, term.ErrBack) || errors.Is(err, term.ErrCancelled) {
		return
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	signal, ok := pickSignal(in)
	if !ok {
		return
	}

	fmt.Printf("\nSending %s to PID %d in %s...\n", signal, process.PID, describe(picked))

	killCtx, cancelKill := context.WithTimeout(context.Background(), timeout)
	defer cancelKill()

	result, err := docker.KillProcess(killCtx, picked.ID, process.PID, signal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showExec(result)

	if result.ExitCode == 0 && process.PID != 1 {
		fmt.Print("\nSignalled. The container is still up with one process gone, which\n")
		fmt.Print("is the state a supervisor, a worker pool or a client's idle\n")
		fmt.Print("connections have to notice on their own.\n")
	}
}

func breakDNS(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	nameserver, err := promptText(in, "Nameserver to point at", docker.BrokenNameserver)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	result, err := docker.BreakDNS(ctx, picked.ID, nameserver)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showExec(result)

	if result.ExitCode != 0 {
		return
	}

	fmt.Print("\nResolution in this container now goes nowhere: every lookup waits\n")
	fmt.Print("for the resolver timeout and then fails. Connections already open\n")
	fmt.Print("are unaffected, so this hits reconnects and new clients only -- the\n")
	fmt.Print("failure that shows up minutes after the thing that caused it.\n")
}

func restoreDNS(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	result, err := docker.RestoreDNS(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showExec(result)
}

func blackhole(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	host, err := promptText(in, "Hostname to blackhole (e.g. the database's service name)", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if host == "" {
		fmt.Print("\nNo hostname given; nothing changed.\n")
		return
	}

	address, err := promptText(in, "Address to send it to", docker.BlackholeAddress)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	result, err := docker.Blackhole(ctx, picked.ID, host, address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showExec(result)

	if result.ExitCode != 0 {
		return
	}

	fmt.Printf("\n%s now resolves to %s inside %s, which is routed nowhere: a\n", host, address, describe(picked))
	fmt.Print("connection to it hangs until it times out rather than being refused.\n")
	fmt.Print("Only this container sees it -- /etc/hosts is a per-container mount.\n")
}

func clearBlackholes(in *bufio.Reader) {
	picked, ok := pickRunning(in, backHint)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	result, err := docker.ClearBlackholes(ctx, picked.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	showExec(result)
}

// ---------------------------------------------------------------- pickers

// submenu runs one group of actions until the user steps back out of it.
func submenu(in *bufio.Reader, heading string, actions []action) {
	choices := make([]term.Choice[int], 0, len(actions))
	for i, a := range actions {
		choices = append(choices, term.Choice[int]{Label: a.Label, Value: i})
	}

	for {
		picked, err := term.Select(in, "\n"+title+"\n"+heading+"\n"+backHint, choices)
		if errors.Is(err, term.ErrBack) || errors.Is(err, term.ErrCancelled) {
			return
		}

		if err != nil {
			fmt.Fprintln(os.Stderr, "shipwreck:", err)
			return
		}

		actions[picked].Run(in)
	}
}

func pickContainer(in *bufio.Reader, hint string) (docker.Container, bool) {
	return pickContainerWhere(in, hint, nil, "no containers")
}

// Only a running container can be exec'd into or frozen, so those actions are
// spared a picker full of choices the daemon would reject.
func pickRunning(in *bufio.Reader, hint string) (docker.Container, bool) {
	return pickContainerWhere(in, hint, running, "no running containers")
}

func pickContainerWhere(in *bufio.Reader, hint string, keep func(docker.Container) bool, empty string) (docker.Container, bool) {
	fmt.Print("\nFetching containers...\n")

	all, err := listForPicker()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return docker.Container{}, false
	}

	containers := all
	if keep != nil {
		containers = nil

		for _, c := range all {
			if keep(c) {
				containers = append(containers, c)
			}
		}
	}

	if len(containers) == 0 {
		fmt.Printf("\n%s\n", empty)
		return docker.Container{}, false
	}

	columns, rows := docker.Rows(containers)

	choices := make([]term.Choice[docker.Container], 0, len(containers))
	for i, c := range containers {
		choices = append(choices, term.Choice[docker.Container]{Label: rows[i], Value: c})
	}

	header := "\n" + title + "\n" + hint + "\n\n   " + columns

	picked, err := term.Select(in, header, choices)
	if errors.Is(err, term.ErrBack) || errors.Is(err, term.ErrCancelled) {
		return docker.Container{}, false
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return docker.Container{}, false
	}

	return picked, true
}

func pickNetwork(in *bufio.Reader, hint string, networks []docker.Network, attached map[string]docker.EndpointSettings) (docker.Network, bool) {
	columns, rows := docker.NetworkRows(networks, attached)

	choices := make([]term.Choice[docker.Network], 0, len(networks))
	for i, n := range networks {
		choices = append(choices, term.Choice[docker.Network]{Label: rows[i], Value: n})
	}

	header := "\n" + title + "\n" + hint + "\n\n   " + columns

	picked, err := term.Select(in, header, choices)
	if errors.Is(err, term.ErrBack) || errors.Is(err, term.ErrCancelled) {
		return docker.Network{}, false
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return docker.Network{}, false
	}

	return picked, true
}

func pickSignal(in *bufio.Reader) (string, bool) {
	choices := []term.Choice[string]{
		{Label: "SIGTERM  -- ask it to shut down, and find out if it does", Value: "TERM"},
		{Label: "SIGKILL  -- take it away with no cleanup", Value: "KILL"},
		{Label: "SIGSTOP  -- freeze this one process, leave the rest running", Value: "STOP"},
		{Label: "SIGCONT  -- resume a process stopped earlier", Value: "CONT"},
	}

	header := "\n" + title + "\nup/down to move, Enter to send, b or q to go back"

	picked, err := term.Select(in, header, choices)
	if errors.Is(err, term.ErrBack) || errors.Is(err, term.ErrCancelled) {
		return "", false
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return "", false
	}

	return picked, true
}

func listForPicker() ([]docker.Container, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return docker.List(ctx)
}

func running(c docker.Container) bool {
	return c.State == "running"
}

func paused(c docker.Container) bool {
	return c.State == "paused"
}

// ---------------------------------------------------------------- prompts

func promptInt(in *bufio.Reader, label string, def int) (int, error) {
	line, err := promptLine(in, fmt.Sprintf("%s (blank for %d)", label, def))
	if err != nil {
		return 0, err
	}

	if line == "" {
		return def, nil
	}

	n, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", line)
	}

	return n, nil
}

func promptFloat(in *bufio.Reader, label string, def float64) (float64, error) {
	line, err := promptLine(in, fmt.Sprintf("%s (blank for %g)", label, def))
	if err != nil {
		return 0, err
	}

	if line == "" {
		return def, nil
	}

	n, err := strconv.ParseFloat(line, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", line)
	}

	return n, nil
}

func promptText(in *bufio.Reader, label string, def string) (string, error) {
	if def != "" {
		label = fmt.Sprintf("%s (blank for %s)", label, def)
	}

	line, err := promptLine(in, label)
	if err != nil {
		return "", err
	}

	if line == "" {
		return def, nil
	}

	return line, nil
}

func promptLine(in *bufio.Reader, label string) (string, error) {
	fmt.Printf("\n%s: ", label)

	line, err := in.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}

	return strings.TrimSpace(line), nil
}

// ---------------------------------------------------------------- output

// Prints what a command inside the container said. A non-zero exit is the
// container's answer, not a shipwreck failure, so it is reported rather than
// raised.
func showExec(result docker.ExecResult) {
	if out := result.Output(); out != "" {
		fmt.Printf("\n%s\n", out)
	}

	if result.ExitCode != 0 {
		fmt.Printf("\nexit %d\n", result.ExitCode)
	}
}

func showWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Printf("\nwarning: %s\n", w)
	}
}

// The daemon's status, plus the two details that say what a squeeze did: an OOM
// kill, and the code the process died with.
func describeState(s docker.State) string {
	state := s.Status

	if s.OOMKilled {
		state += ", OOM killed"
	}

	if !s.Running && !s.Restarting && s.ExitCode != 0 {
		state += fmt.Sprintf(", exit %d", s.ExitCode)
	}

	return state
}

func describe(c docker.Container) string {
	if name := c.Name(); name != "" {
		return name
	}

	return c.ShortID()
}

func abandonShip() {
	fmt.Println("\nPaddling away!")
	fmt.Print(exit)
}

func loadMenuChoices() []term.Choice[int] {
	options := loadMenuOptions()

	choices := make([]term.Choice[int], 0, len(options))
	for _, o := range options {
		choices = append(choices, term.Choice[int]{Label: string(o.Value), Value: int(o.Number)})
	}

	return choices
}

func loadMenuOptions() []MenuOption[int, string] {
	return []MenuOption[int, string]{
		{Number: 1, Value: "Get Containers"},
		{Number: 2, Value: "Sigkill Container"},
		{Number: 3, Value: "Sigterm Container"},
		{Number: 4, Value: "Stop Container (drain timeout)"},
		{Number: 5, Value: "Restart Container"},
		{Number: 6, Value: "Freeze Container (pause / unpause)"},
		{Number: 7, Value: "Partition Network (disconnect / reconnect)"},
		{Number: 8, Value: "Squeeze Resources (memory / cpu / pids / blkio)"},
		{Number: 9, Value: "In-Container Faults (cpu / disk / kill / dns / hosts)"},
		{Number: 10, Value: "Abandon Ship! (Exit)"},
	}
}
