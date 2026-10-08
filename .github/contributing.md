# Contributing

Read [docs/architecture.md](../docs/architecture.md) for the package map and the
boundaries. [AGENTS.md](../AGENTS.md) holds the rules every change follows.

## Set up

Follow [Install](../readme.md#install) in the README. `mise install` installs
the versions pinned in [`mise.toml`](../mise.toml), including Zig and Bun. Zig
is the C compiler for `go test -race`, which needs cgo, so a clean host needs no
`gcc`; Bun runs the Markdown formatter. The latex tests also need `prlimit` from
util-linux on `PATH`.

## Tasks

| Task                     | Does                                                           |
| ------------------------ | -------------------------------------------------------------- |
| `mise run build`         | Builds `bin/botkit-flow` and `bin/botkit-latex`.               |
| `mise run test`          | Runs `go test -race ./...` with Zig as the C compiler.         |
| `mise run install:bin`   | Builds and installs both bots into `$PREFIX/bin`.              |
| `mise run install:units` | Installs the systemd units into `$UNIT_DIR`.                   |
| `mise run docs:format`   | Formats the Markdown files.                                    |
| `mise run lint:check`    | Verifies the lint config and checks formatting and lint rules. |
| `mise run lint`          | Formats the code and applies lint fixes.                       |
| `mise run ci`            | Runs `build`, `lint:check`, and `test`.                        |
| `mise run env:example`   | Rewrites `.env.example` from the settings the bots read.       |
| `mise run dev:flow`      | Runs the flow bot from source, loading `.env`.                 |
| `mise run dev:latex`     | Runs the latex bot from source, loading `.env`.                |

`install:bin` installs into `$PREFIX/bin` (default `/usr/local/bin`) and
`install:units` into `$UNIT_DIR` (default `/etc/systemd/system`).

Run `mise run ci` before you open a pull request. GitHub Actions runs the build,
`go vet`, the tests, and golangci-lint.

## Tests

Tests run the real apps over SQLite and the in-memory transport in
`internal/whatsapp/fake`, and never connect to WhatsApp. The latex tests run the
real `typst`.

## Settings

A bot's settings come from the code that reads them. After you add or change
one, give it an explanation in
[`internal/envfile`](../internal/envfile/envfile.go), run
`mise run env:example`, and update
[docs/configuration.md](../docs/configuration.md). `mise run test` fails while
`.env.example` is out of date.

## Documentation

Format Markdown to 80 columns:

```bash
mise run docs:format
```
