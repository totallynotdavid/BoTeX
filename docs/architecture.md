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
and history. Its per-user lock is in memory. The built-in flow persists the last
choice and explicit follow-up consent, reacts only after a successful action,
and exposes a rate-limited `FollowUp` method called by the production hourly
scheduler.

### Flow state and concurrency

The complete `user_state` state is the tuple below; `conversation_history` is
the append-only record of inbound and outbound text, including scheduler
reminders.

| State                                                      | Meaning                                                   | Writers and authorized transitions                                                                                                                                                                                                                                                 |
| ---------------------------------------------------------- | --------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `current_node`                                             | Current FSM node                                          | An inbound `Store.Turn` may move it along a matching node/global transition, restart stale/unknown state at `start_node`, or leave it on fallback. The follow-up scheduler never changes it.                                                                                       |
| `user_name`                                                | Name from profile or `save_user_name`                     | The inbound turn on a name transition sets, clears, or retains it. Scheduler turns retain it.                                                                                                                                                                                      |
| `course_interest`, `selected_course_id`, `consulted_price` | Lead facts                                                | Only their named inbound actions set them; scheduler turns retain them.                                                                                                                                                                                                            |
| `last_choice`                                              | Last guide choice                                         | An inbound choice action sets it. Scheduler turns read it for the reminder and never change it.                                                                                                                                                                                    |
| `follow_up_opt_in`                                         | Explicit reminder consent                                 | An inbound `opt_in_follow_up` transition sets `true`; an inbound `opt_out_follow_up` transition sets `false`. The scheduler only reads it.                                                                                                                                         |
| `last_follow_up`                                           | UTC timestamp of the current claim or successful reminder | The scheduler claims an eligible opted-in row by setting it to `now`; after `SendText` succeeds it remains the send attempt. A failed send releases the matching claim to `NULL`. Inbound turns never change it. An opt-out after claim does not cancel that already-claimed send. |
| `voucher_path`, `requires_human_agent`                     | Voucher and hand-off flag                                 | Inbound actions update them; scheduler turns retain them.                                                                                                                                                                                                                          |
| `last_updated`                                             | Last committed inbound turn                               | Every inbound `Store.Turn` writes it, including fallback and unsupported-media turns. Claim, release, and follow-up history writes preserve it, so a reminder cannot reset the 24-hour stale-conversation clock.                                                                   |

Every state transition is serialized by the in-process per-user lock. Inbound
turns commit state and their history rows together and update `last_updated`.
For restart decisions, a claim in `last_follow_up` newer than `last_updated`
also counts as recent activity: a reply to a reminder resumes the live flow, but
a user returning after that reminder is stale starts at `start_node` again. The
scheduler claims with a state-only write that preserves `last_updated`, sends
outside the lock, then appends a successful reminder to history without touching
state. The claim is the send attempt: if sending fails, it releases only the
matching timestamp to `NULL`; if sending succeeds, the claim remains even when
the history append fails, preserving at-most-once delivery. An inbound opt-out
that happens after claim does not cancel the already-claimed send. The
deployment must run one process per database; the lock is not cross-process.

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
