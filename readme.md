# botkit

botkit is a pair of WhatsApp bots for teams that need a small, self-hosted chat
workflow. It is written in Go and stores its WhatsApp session and bot state in
SQLite. The `flow` bot walks people through a JSON conversation. The `latex` bot
turns a math expression into a PNG.

The first working path is the flow bot:

```bash
mise install
mise run build
mise exec -- bin/botkit-flow pair
mise exec -- bin/botkit-flow run
```

Send the paired account a direct message. With no `FLOW_FILE`, the bot runs the
built-in bookshop example. Pairing needs a terminal and a WhatsApp account. The
complete operating procedure is in [docs/operations.md](docs/operations.md).

## The bots

`flow` answers direct messages from anyone. It keeps each conversation in the
SQLite store and can save payment-voucher images. Start with
[docs/flow.md](docs/flow.md) when you need to write a flow of your own.

`latex` answers `!latex <equation>` in direct chats and registered groups. It
requires a registered user or owner and renders with Typst and the embedded
Mitex package. See [docs/latex.md](docs/latex.md) for access and rendering
limits.

Both bots share the same WhatsApp session handling, SQLite store, rate limiter,
configuration, and process lifecycle. The settings are in
[docs/configuration.md](docs/configuration.md).

## Manual and development

The manual index is [docs/readme.md](docs/readme.md). Contributors should run
the checks in [docs/development.md](docs/development.md). The package and
runtime boundaries are in [ARCHITECTURE.md](ARCHITECTURE.md).
