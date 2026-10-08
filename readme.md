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

## Engaging conversations and offline use

The `latex` redesign follows three useful patterns from working WhatsApp bots:

| Reference                                                                           | What makes it engaging                                                                                     | Applied here                                                                                                          |
| ----------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| [Jibaru/wspbot](https://github.com/Jibaru/wspbot)                                   | A clear personality, opt-in group behavior, memory, useful media, reactions, and short confirmations.      | `latex` has the authorized Tinta welcome, persisted first-visit state, emoji feedback, and image replies.             |
| [miftahganzz/bot-wa-go](https://github.com/miftahganzz/bot-wa-go)                   | A discoverable menu, command aliases, modular features, media tools, games, and explicit runtime feedback. | `latex` exposes discoverable help/menu text, command routing, rendering, and success, failure, and refusal reactions. |
| [avig14/whatsapp-restaurant-bot](https://github.com/avig14/whatsapp-restaurant-bot) | A guided multi-step flow with a cart-like state, confirmation, recovery, and a useful media-rich result.   | Research only here; this change does not claim to redesign `flow`.                                                    |

The shared transport boundary is `bot.Transport`. The WhatsApp implementation is
only one transport; `whatsapp/fake` is an in-memory transport that records text,
images, reactions, and downloaded media. That fake drives the full conversation
tests, so application behavior does not require a WhatsApp login.

Try either bot offline:

```bash
go run ./cmd/flow --repl
go run ./cmd/latex --repl
```

`flow` is included here only because it also gains the shared offline REPL; its
conversation redesign is outside this change.

Type messages at `you>`. The bot replies at `bot>`, and `:quit` exits. The REPL
uses the real app, SQLite state, rate limiting, command routing, rendering, and
fake transport; it does not create a WhatsApp connection or require pairing. The
user is assigned the first configured non-owner rank by default (`user` for
`latex`; `flow` configures no non-owner rank); add `--owner` when exercising
owner commands locally.

### The real-session boundary

Exactly one part still requires a real WhatsApp session: the production
transport in `internal/whatsapp`, including QR/pairing, socket connection and
reconnection, WhatsApp event translation, encrypted media upload/download, and
delivery to an actual phone or group. `pair` and the default `run` command use
that transport and therefore need a paired account.

We could not test an actual WhatsApp login, server reconnect, device logout or
replacement, WhatsApp-side delivery/read behavior, or the real CDN encryption
and media limits in this environment. Those paths are isolated behind
`bot.Transport`; the app conversations, command routing, rendering, media
handling through the fake, and offline REPL are tested without them.

## Manual and development

The manual index is [docs/readme.md](docs/readme.md). Contributors should run
the checks in [docs/development.md](docs/development.md). The package and
runtime boundaries are in [ARCHITECTURE.md](ARCHITECTURE.md).
