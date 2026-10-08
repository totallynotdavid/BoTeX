# Contributing

Read [docs/architecture.md](../docs/architecture.md) for the package map and the
boundaries. [AGENTS.md](../AGENTS.md) holds the rules every change follows.

## Set up

botkit uses Go and [mise](https://mise.jdx.dev). `mise install` installs the
versions pinned in [`mise.toml`](../mise.toml): Go, golangci-lint, Typst, and
the `sqlite3` CLI. The latex tests also need `prlimit` from util-linux on
`PATH`.

```bash
git clone https://github.com/totallynotdavid/botkit
cd botkit
mise install
mise run build
```

## Tasks

| Task                   | Does                                                           |
| ---------------------- | -------------------------------------------------------------- |
| `mise run build`       | Builds `bin/botkit-flow` and `bin/botkit-latex`.               |
| `mise run test`        | Runs `go test -race ./...`.                                    |
| `mise run lint:check`  | Verifies the lint config and checks formatting and lint rules. |
| `mise run lint`        | Formats the code and applies lint fixes.                       |
| `mise run ci`          | Runs `build`, `lint:check`, and `test`.                        |
| `mise run env:example` | Rewrites `.env.example` from the settings the bots read.       |
| `mise run dev:flow`    | Runs the flow bot from source, loading `.env`.                 |
| `mise run dev:latex`   | Runs the latex bot from source, loading `.env`.                |

Run `mise run ci` before you open a pull request. GitHub Actions runs the build,
`go vet`, the tests, and golangci-lint.

## Tests

Tests do not connect to WhatsApp. They run the real apps over SQLite and the
in-memory transport in `internal/whatsapp/fake`. The latex tests run the real
`typst`.

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
bunx prettier --print-width 80 --prose-wrap always --write '**/*.md'
```
