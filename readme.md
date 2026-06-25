# go-build-cache

A shared Go build cache as a single binary with two subcommands:

- `go-build-cache serve` — HTTP cache server, local disk L1 in front of an S3 (e.g. SeaweedFS) L2. Holds the S3 credentials; runs as a host service.
- `go-build-cache cacheprog` — the `GOCACHEPROG` child process the go toolchain runs. Talks HTTP to the server (credential-free) and falls back to local disk if the server is unreachable.

It implements cmd/go's `GOCACHEPROG` stdin/stdout protocol and a small HTTP cache protocol itself — no third-party cache dependency.

## Usage

Point `GOCACHEPROG` at the client; on CI runners it finds the host cache server via the gateway and otherwise uses local disk:

```sh
export GOCACHEPROG="go-build-cache cacheprog --gateway-addr-port=5930"
go build ./...
```

Run the server on the host:

```sh
go-build-cache serve --listen=:5930 --cache-dir=/var/cache/go-build-cache \
  --bucket=go-build-cache --s3-endpoint-url=https://s3.example.com --s3-path-style
```

## Releases

Tagging `vX.Y.Z` builds static linux amd64/arm64 binaries (GitHub release) and a
multi-arch image at `ghcr.io/cego/go-build-cache` via GoReleaser.
