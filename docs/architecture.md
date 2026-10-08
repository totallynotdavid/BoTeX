# Architecture

botkit has two binaries and one shared runtime. A binary describes its bot. The
runtime owns the WhatsApp connection, message filtering, concurrency, shutdown,
and exit status.

```text
cmd/latex ─┐
           ├─ internal/cli ─ internal/bot ─ internal/whatsapp ─ WhatsApp
cmd/flow  ─┘      │               │
                  │               └─ internal/ratelimit
                  ├─ internal/config
                  ├─ internal/auth ─┐
                  └─ internal/sqlite ┴─ SQLite

cmd/latex ─ internal/command ─ internal/latex ─ internal/typst
cmd/flow  ─ internal/flow ─ internal/flow/fsm
```

## Packages

| Package                                               | Owns                                                                                     |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| [`cmd/latex`](../cmd/latex/main.go)                   | The latex bot's ranks, its `!` command router, and group support.                        |
| [`cmd/flow`](../cmd/flow/main.go)                     | The flow bot's flow loading, store, and actions. It accepts direct messages only.        |
| [`internal/cli`](../internal/cli)                     | The subcommands `run`, `pair`, and `--repl`, the assembly of a bot, and the exit status. |
| [`internal/config`](../internal/config)               | Typed environment settings. It collects every malformed value before anything is opened. |
| [`internal/envfile`](../internal/envfile)             | `.env.example`, generated from the settings the bots read.                               |
| [`internal/bot`](../internal/bot)                     | The runtime: events, filtering, handler goroutines, the `Transport` interface.           |
| [`internal/whatsapp`](../internal/whatsapp)           | The only package that imports whatsmeow: connection, pairing, event translation.         |
| [`internal/whatsapp/fake`](../internal/whatsapp/fake) | An in-memory `Transport` for the REPL and the tests.                                     |
| [`internal/sqlite`](../internal/sqlite)               | Opening the shared database with foreign keys, WAL mode, and a busy timeout.             |
| [`internal/ratelimit`](../internal/ratelimit)         | The per-user sliding-window limiter.                                                     |
| [`internal/auth`](../internal/auth)                   | Users, ranks, registered groups, and the authorization decision.                         |
| [`internal/command`](../internal/command)             | Routing `!name args` to commands, the permission check, reactions, and `help`.           |
| [`internal/latex`](../internal/latex)                 | The `latex` command, its limits, the embedded Mitex package, and the welcome.            |
| [`internal/typst`](../internal/typst)                 | Running `typst` under resource limits in a private directory.                            |
| [`internal/flow`](../internal/flow)                   | User state, conversation history, actions, templates, and replies.                       |
| [`internal/flow/fsm`](../internal/flow/fsm)           | Parsing and validating a flow, and choosing the next node.                               |
| [`internal/flow/names`](../internal/flow/names)       | Picking a first name from typed text or a profile name.                                  |

## Assembly

A binary builds a [`cli.Command`](../internal/cli/cli.go) and calls `cli.Main`.
`internal/cli` reads the settings, opens SQLite, sets up authentication, seeds
the owners, builds the app, creates the rate limiter, opens the WhatsApp
transport, and starts the runtime. The binaries hold no connection or process
lifecycle code.

## Runtime boundary

`internal/bot` receives events from a `Transport` and hands accepted messages to
the `App` on handler goroutines. It drops own, group, non-allowed, rate-limited,
and over-capacity messages before the app sees them. It answers a refused
message from a separate pool of four, so a flood cannot take a slot from real
work.

The runtime owns one lifetime context. A caller cancellation stops the run. A
session end stops it and returns a
[`SessionEndedError`](../internal/bot/event.go), which `internal/cli` turns into
exit status 78.

`bot.Transport` is the boundary to WhatsApp. `internal/whatsapp` implements it
over whatsmeow, and `internal/whatsapp/fake` implements it in memory. A lint
rule in [`.golangci.toml`](../.golangci.toml) stops other packages from
importing whatsmeow.

## Authentication and commands

`internal/auth` checks a user, then the rank's command list, then the group
registration for a group message. A direct chat needs no group registration. The
`owner` rank lists every command. The router in `internal/command` turns the
decision into a reply or runs the command.

The flow bot defines no rank and does not call `internal/auth` to decide.

## Flow app

`internal/flow/fsm` parses, validates, and routes. `internal/flow` applies one
message as one turn: [`Store.Turn`](../internal/flow/store.go) loads the user's
state, runs the turn, and saves the state and history in one transaction. The
app sends the replies after the save. The store is the only writer of flow state
and history. Its per-user lock is in memory.

## Latex app

`internal/latex` passes one expression to `internal/typst`, which runs `typst`
with the embedded Mitex package and never downloads packages. The command turns
empty input, invalid input, and limit violations into chat notices. The router
adds the reaction and returns the error to the runtime. The app also answers
greetings and keeps the `latex_user_state` table.

## Data ownership

```text
internal/whatsapp  session tables used by whatsmeow
internal/auth      users, ranks, registered_groups
internal/flow      user_state, conversation_history, voucher files in FLOW_VOUCHER_DIR
internal/latex     latex_user_state
```

The CLI opens one SQLite database for the session and the app's data.
