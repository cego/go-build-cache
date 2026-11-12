# go-build-cache

A shared Go build cache for CI. Compiled build and test artifacts are stored
once on a server and reused across machines and ephemeral runners, so
`go build`/`go test` skip recompiling what an earlier run — or another runner —
already built.

One binary, two subcommands:

- **`go-build-cache serve`** — an HTTP cache server: local disk (L1) in front of
  an S3 bucket (L2, e.g. SeaweedFS). Holds the S3 credentials and runs as a host
  service.
- **`go-build-cache cacheprog`** — the
  [`GOCACHEPROG`](https://pkg.go.dev/cmd/go/internal/cacheprog) child process the
  Go toolchain runs (Go 1.24+). It talks to the server without credentials and
  falls back to the local build cache when the server is unreachable, so builds
  never fail because of it.

## Use it

Run the server on the host / CI runner:

```sh
go-build-cache serve --listen=:5930 --cache-dir=/var/cache/go-build-cache \
  --bucket=go-build-cache --s3-endpoint-url=https://s3.example.com --s3-path-style
```

Point the Go toolchain at it:

```sh
export GOCACHEPROG="go-build-cache cacheprog --cache-server=http://<server-host>:5930"
go build ./...
```

Each request to the server times out after 30s, after which `cacheprog` uses
the local build cache only. Override with `GO_BUILD_CACHE_TIMEOUT`, e.g.
`GO_BUILD_CACHE_TIMEOUT=2m`.

## Install

- Binaries (static, linux amd64/arm64): GitHub Releases.
- Container image: `ghcr.io/cego/go-build-cache`.
