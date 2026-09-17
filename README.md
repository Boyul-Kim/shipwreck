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
>
> AI use: AI helped with this documentation and the `internal/term` package.
> Everything else was written by hand.

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
