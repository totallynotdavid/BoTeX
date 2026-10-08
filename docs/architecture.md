# Architecture

botkit has two binaries and one shared runtime. A binary describes its bot. The
runtime owns the WhatsApp connection, message filtering, concurrency, shutdown,
and exit status. This document maps the code. For what the bots do, read
[the latex bot](latex-bot.md) and [the flow bot](flow-bot.md).

The code, not this document, says which package imports which. Print each
package with its imports:

```bash
mise exec -- go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./...
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
and history. Its per-user lock is in memory, which is why
[one process runs on each database](running.md).

### Flow state

`user_state` holds one row per user. `conversation_history` is the append-only
record of text in both directions, including reminders.

| Column                                                     | Meaning                                       | Written by                                                                                                                              |
| ---------------------------------------------------------- | --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| `current_node`                                             | Current node of the flow                      | An inbound turn: a matching transition, a restart at `start_node`, or no change on a fallback.                                          |
| `user_name`                                                | Name from the profile or `save_user_name`     | An inbound turn on a name transition: set, cleared, or kept.                                                                            |
| `course_interest`, `selected_course_id`, `consulted_price` | Lead facts                                    | Only their named inbound actions.                                                                                                       |
| `last_choice`                                              | Last choice in the guide                      | An inbound `remember_choice` action. The reminder reads it.                                                                             |
| `follow_up_opt_in`                                         | Consent to reminders                          | An inbound `opt_in_follow_up` (true) or `opt_out_follow_up` (false). The scheduler only reads it.                                       |
| `last_follow_up`                                           | UTC time of the latest claim or reminder sent | The scheduler. See [Reminders](#reminders). Inbound turns never change it.                                                              |
| `voucher_path`, `requires_human_agent`                     | Latest voucher and the hand-off flag          | Inbound actions.                                                                                                                        |
| `last_updated`                                             | Time of the last committed inbound turn       | Every inbound turn, including fallbacks and unsupported media. A reminder never changes it, so it cannot reset the 24-hour clock below. |

An inbound turn writes the row and appends to `conversation_history`. The
scheduler changes one column, `last_follow_up`, when it claims a reminder and
when it releases the claim ([Reminders](#reminders)). It never changes
`current_node`, `user_name`, the lead facts, `last_choice`, the consent, the
voucher or hand-off columns, or `last_updated`. After a send it only appends to
`conversation_history`. The per-user lock serializes every write.

A conversation is stale when the later of `last_updated` and `last_follow_up` is
more than 24 hours old. A stale conversation restarts at `start_node`.

A claim newer than `last_updated` counts as activity. A reply to a reminder
therefore resumes the live flow. A user who returns after that day starts again.

### Reminders

[`App.FollowUp`](../internal/flow/app.go) sends one reminder to each user who
has opted in and has had none for 24 hours. `runFollowUps` in
[`internal/cli/run.go`](../internal/cli/run.go) calls it every hour while `run`
is active. A reminder moves in three steps:

1. **Claim.** Under the lock, the store sets `last_follow_up` to `now` for each
   eligible user. The write leaves `last_updated` alone.
2. **Send.** The app sends the text outside the lock. If the send fails, the
   store clears `last_follow_up` for that claim, and a later check retries.
3. **Record.** After a send that succeeds, the store appends the reminder to
   `conversation_history`. If that append fails, the claim stays, so the user
   gets the reminder at most once.

A user who opts out after the claim still gets the reminder already claimed.

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
