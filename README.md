```text
                   |\
                   | \__
                   |    \_
                   |__/\__\
              _____|____
              \  /\    /
               \/  \__/
                   |                                  _____
                   |                                 /_____\
            _______|______                           ( o_o )
       ____/______________\_                        __/|_|\
       \  O   O   O     _/  \                      /   | | \__
        \             _/    /                 ____/____|_|____\_____
         \______  ___/ \___/                  \                \   /  --->
  ~~~~~~~~~~~~~\/~~~~~~~~~~~~~~~~~~            \________________\_/
       ~~~~~           ~~~~~          ~~~~~~~~~~~~       ~~~~~   \
                            ~~~~             ~~~~~                \___
                                                                   \__\\
```

# shipwreck

A zero-dependency test harness for the failure paths your integration tests never
execute. shipwreck injects faults into the Docker containers your application
depends on, so you find the missing timeout on your laptop instead of in an
incident review.

> **Status: in active development.**

## Why

Trying to fill a gap between unit tests and integration tests.

That branch is where the bugs live:

- retries with no backoff or jitter, or retries on a write that isn't idempotent
- connection pools exhausted when a dependency is **slow** rather than down,
  which never happens locally because locally everything is sub-millisecond
- error branches that have never once executed, so they carry their own nil
  dereferences
- "the database is up when I start" assumptions, which survive until the first
  change in startup order
- liveness probes that return `200` while the thing behind them is dead, so the
  orchestrator never restarts anything
- `SIGTERM` handlers that were never exercised, because you always hit Ctrl-C

Every one of those is deterministic given a single fault and a single request. shipwreck exists to make that path cheap enough to test that you actually test it.

## What this is not

shipwreck is a **pre-production** tool.

Failures that emerge from scale — retry storms, cascading saturation, capacity
cliffs, split-brain under a partial partition — need real traffic and real
topology to reproduce. A laptop cannot find them, and shipwreck does not claim to.
Those belong to production chaos engineering, with the blast-radius controls that
implies.

shipwreck covers the other half: single-request failure modes that reproduce
against one container on one machine, and that go untested today because the
tooling for them assumes a cluster, a platform team, and a budget.

Coverage for the error path, not a miniature of production.

## Getting started

### Requirements

- **Go 1.26+** — the module targets `go 1.26.4`.
- **Docker Engine 20.10+**, running locally. shipwreck talks to the Engine API at
  version `v1.41` over the local socket or named pipe; it does not shell out to the
  `docker` CLI and pulls in no third-party modules.
- **GNU Make** (optional) for the build targets below. On Windows it ships with Git
  for Windows — run `make` from Git Bash, not `cmd.exe` or PowerShell.

Confirm Docker is reachable before you start:

```sh
docker version
```

### Build and run

```sh
git clone https://github.com/Boyul-Kim/shipwreck.git
cd shipwreck

make run
```

Or build a binary:

```sh
make build
./bin/shipwreck
```

Without Make:

```sh
go run ./cmd/shipwreck_cli
go build -o bin/shipwreck ./cmd/shipwreck_cli
go install ./cmd/shipwreck_cli    # -> $(go env GOPATH)/bin
```

### Connecting to Docker

The socket is discovered automatically:

| Platform | Default |
| --- | --- |
| Windows | `npipe:////./pipe/docker_engine` |
| Linux / macOS | the first of `/var/run/docker.sock`, `$XDG_RUNTIME_DIR/docker.sock` (rootless), or `~/.docker/run/docker.sock` (Docker Desktop) that exists |

Set `DOCKER_HOST` to override it. `unix://`, `npipe://`, `tcp://`, and `http://`
schemes are supported:

```sh
DOCKER_HOST=unix:///var/run/docker.sock ./bin/shipwreck
DOCKER_HOST=tcp://127.0.0.1:2375 ./bin/shipwreck
```

## Faults

Every fault is one menu entry: pick it, pick a container, answer at most two
prompts. Nothing is scheduled and nothing runs in the background — a fault is
applied when you choose it and stays until you undo it.

### Lifecycle

| Fault | What the dependency looks like |
| --- | --- |
| Sigkill | gone without a goodbye: no `SIGTERM` handler runs, no connection is closed cleanly |
| Sigterm | asked to shut down, so you find out whether it actually does |
| Stop (drain timeout) | `SIGTERM`, then `SIGKILL` after the timeout you give |
| Restart | gone and then back, which is where "the database is up when I start" dies |

### Freeze

`Pause` suspends every process in the container with the cgroup freezer. The
important part is what it does **not** do: the container keeps its listening
sockets and its open connections. A client connects, sends a request, and waits
forever — no `RST`, no `EOF`, no error to branch on.

That is the hung dependency, and it is the single cheapest way to find a request
without a timeout. `Unpause` thaws it and everything queued arrives at once.

### Partition

`Disconnect from a Network` cuts one container off one network, with `Force` so
it works on a running container.

Because it is per network, a container on more than one network gets a **partial
partition**: isolate a service from the database network and it still answers
your API network normally, while everything it tries to do with the database
hangs. That asymmetry is what breaks health checks that only test the process.

`Reconnect to a Network` heals it and restores the network aliases the
disconnect dropped, so service discovery works under the same name as before.
The address is only restored if the endpoint had a static one — a dynamic
address may well have been handed to something else by then, and the default
bridge rejects a requested address outright.

### Squeeze

Resource limits go through `POST /containers/{id}/update`, so the container is
not restarted and keeps its PID, its connections and its state.

| Squeeze | What it provokes |
| --- | --- |
| Memory limit | swap is pinned to the same value, so the next allocation past the cap gets the OOM killer rather than a slow spill |
| CPU limit | a noisy neighbour: everything still works, only slowly, which is what fills connection pools |
| PID limit | `fork` and thread creation fail — an error path most runtimes have never executed |
| Blkio weight | a smaller share of disk bandwidth, when something else is competing for the same disk |

The original limits are read from `GET /containers/{id}/json` before the first
squeeze of a container and printed, so they are in your scrollback, and
`Restore Limits` puts them back.

Two caveats worth knowing before you rely on restore:

- The originals are held **in memory for this session only**. `update`
  overwrites the container's `HostConfig`, so once shipwreck exits they are gone
  from everywhere, and a restart of the container will not bring them back.
  `Show Recorded Limits` prints both the live and the recorded values.
- A limit that was *never set* is not always expressible. The update API reads a
  zero as "leave alone", so unlimited has to be said with `-1` — which works for
  CPU and PIDs, but not for memory, where the daemon's floor is 6MB. If a
  container started with no memory limit, clearing the one you set needs the
  container recreated. shipwreck says so instead of pretending it restored.

### In-container faults

These run through the exec API (`POST /containers/{id}/exec`, then
`/exec/{id}/start`) and need `/bin/sh` in the image, so they do not work against
a distroless or scratch container. Output and exit status are reported as the
container gave them.

| Fault | Undo |
| --- | --- |
| Burn CPU — detached spin loops that outlive the command that started them | Stop CPU Burn |
| Fill Disk — `dd` a file of the size you pick, on whichever filesystem the path is on | Remove Fill File |
| Kill a Process — pick a specific child by PID, with `SIGTERM`, `SIGKILL`, `SIGSTOP` or `SIGCONT` | — |
| Break DNS — point `/etc/resolv.conf` at a resolver that never answers | Restore DNS |
| Blackhole a Hostname — an `/etc/hosts` entry sending one dependency to unroutable space | Clear Blackholes |

Notes on the ones with sharp edges:

- The process list comes from `/proc` inside the container, not from `ps` (often
  missing in a slim image) and not from `GET /containers/{id}/top`, which reports
  the **host's** PIDs — numbers a `kill` inside the container would apply to the
  wrong process, or to nothing. Killing a child rather than PID 1 is the point:
  the container stays up with one worker gone, which is the state a supervisor or
  a connection pool has to notice by itself. PID 1 is listed, and marked, because
  sometimes that is what you want.
- Burners are tagged in their command line, which is how `Stop CPU Burn` finds
  them again. Pair a burn with a CPU limit to make the squeeze bite immediately.
- `/etc/resolv.conf` and `/etc/hosts` are per-container bind mounts, so both
  faults are invisible to every other container. DNS breakage only affects new
  lookups — connections already open are fine, which is why it tends to surface
  minutes after the change that caused it. Blackholed hostnames point at
  `203.0.113.1` (documentation space, routed nowhere) so connections hang and
  time out instead of being refused immediately.

## Development

```sh
make              # list every target
make check        # fmt + vet + test — run this before committing
make test-race    # tests with the race detector
make cover        # HTML coverage report
make cross        # verify all supported GOOS/GOARCH pairs compile
make release      # release binaries for every platform -> dist/
make clean        # remove bin/, dist/, coverage.out
```

```text
cmd/shipwreck_cli/    main package — the CLI entry point
internal/menu/        interactive menu loop
internal/docker/      Docker Engine API client and table rendering
internal/term/        arrow-key picker and raw terminal mode (unix / windows variants)
internal/dial/        socket discovery and dialing (unix / windows variants)
```
