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

A lightweight, zero-dependency chaos engineering tool for local Docker environments, using only the Docker Engine API.

NOTE: IN ACTIVE DEVELOPMENT

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
internal/dial/        socket discovery and dialing (unix / windows variants)
```