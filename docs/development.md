# Development

The repository uses Go and mise. Install the pinned tools before running the
checks:

```bash
mise install
```

Run the full CI-equivalent task:

```bash
mise run ci
```

The individual tasks are useful when narrowing a change:

```bash
mise run build
mise run lint:check
mise run test
```

`build` writes both bot binaries. `lint:check` verifies the golangci-lint
configuration, formatting, and lint rules without changing files. `test` runs
the Go tests with the race detector. Tests use the fake WhatsApp client and do
not connect to WhatsApp. LaTeX tests run the real Typst binary, so Typst and
`prlimit` must be available on `PATH`.

The source map and package boundaries are in
[ARCHITECTURE.md](../ARCHITECTURE.md). Configuration and operation procedures
are in [configuration.md](configuration.md) and [operations.md](operations.md).
