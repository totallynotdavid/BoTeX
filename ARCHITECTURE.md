# Architecture

botkit has two binaries and one shared runtime. The binaries assemble an app.
The runtime owns the WhatsApp connection, filtering, concurrency, shutdown, and
exit behavior.

```text
cmd/latex ─┐
           ├─ internal/cli ─ internal/bot ─ internal/whatsapp ─ WhatsApp
cmd/flow  ┘        │                │
                    ├─ internal/config
                    ├─ internal/auth ─┐
                    └─ internal/sqlite ┴─ SQLite

cmd/latex ─ internal/command ─ internal/latex ─ internal/typst
cmd/flow  ─ internal/flow ─── internal/flow/fsm
```

## Binaries and assembly

[`cmd/latex`](cmd/latex/main.go) creates a command router with the `user` and
`owner` ranks. It accepts group messages. [`cmd/flow`](cmd/flow/main.go) loads a
flow, creates its store and actions, and accepts direct messages only. Neither
binary owns connection or process lifecycle code.

[`internal/cli`](internal/cli) is the shared assembly layer. It reads
configuration, opens SQLite, initializes authentication, seeds configured
owners, builds the app, creates the rate limiter, opens the WhatsApp client, and
starts the runtime. It also implements the `run` and `pair` subcommands.

[`internal/config`](internal/config) converts environment variables to typed
settings. It records malformed values while reading and returns them together
before the database or WhatsApp client is opened. Bot-specific settings are read
by [`internal/latex`](internal/latex) and [`internal/flow`](internal/flow)
through this package. Each read also records the setting's default, and
[`internal/envfile`](internal/envfile) writes `.env.example` from those records,
so the file never repeats a default by hand.

## Runtime boundary

[`internal/bot`](internal/bot) receives events from the WhatsApp client and
hands accepted messages to the app on handler goroutines. It filters group,
sender, own-message, rate-limit, and in-flight conditions before calling the
app. It limits refusal replies separately from normal handlers.

The runtime owns one lifetime context. A caller cancellation stops the run. A
session-end event stops the run and is returned as a
[`SessionEndedError`](internal/bot/event.go). Handlers receive the cancellation
and get a shutdown grace period. A session end takes precedence over a caller
cancellation when both happen before `Run` returns.

[`internal/whatsapp`](internal/whatsapp) adapts whatsmeow to the runtime's
client and event interfaces. It owns pairing, session-ended reasons, reconnect
events, and the terminal checks for pairing.
[`internal/sqlite`](internal/sqlite) opens the shared database with foreign
keys, WAL mode, and a busy timeout.

## Authentication and commands

[`internal/auth`](internal/auth) owns the `users`, `ranks`, and
`registered_groups` tables. It checks a user, then the rank's command list, then
the group registration for group messages. Direct chats do not require group
registration. The owner rank has every command. The command router in
[`internal/command`](internal/command) turns an authorization decision into a
reply or runs the selected command.

## Flow app

[`internal/flow/fsm`](internal/flow/fsm) parses and validates a flow, compiles
regular expressions, expands transition groups, and selects the next node.
[`internal/flow`](internal/flow) owns user state, conversation history, actions,
and replies. [`Store.Turn`](internal/flow/store.go) serializes turns for one
user and saves state plus history in one SQLite transaction. The app sends
replies after that transaction completes.

The flow app does not keep a user's state between turns. It loads the state,
applies one message, and writes the result. Turns for different users can run in
parallel. The per-user lock is in memory, so two bot processes must not use the
same database.

## LaTeX app

[`internal/latex`](internal/latex) implements the `!latex` command. It passes
one math expression to [`internal/typst`](internal/typst), which runs Typst with
the embedded Mitex package and resource limits. The runner creates a private
temporary directory for each render and removes it when the render ends. It does
not download packages.

The command translates empty input, invalid input, resource limits, and output
size limits into chat notices. The router adds the command's reaction and
returns the underlying error to the runtime.

The app owns the `latex_user_state` table. A row is created atomically when an
authorized user sends a greeting for the first time; later authorized greetings
leave that row unchanged and use the welcome-back message. The `user_id` primary
key and `INSERT ... ON CONFLICT DO NOTHING` make the first-visit transition safe
when concurrent handlers greet the same user: at most one handler observes the
first visit, and `first_seen` never changes. There is no in-memory copy of this
state, so all app instances using the database observe the same transition.
Unauthorized greetings do not read or write this table.

## Data ownership

```text
internal/whatsapp  session tables used by whatsmeow
internal/auth      users, ranks, registered_groups
internal/flow      user_state, conversation_history
internal/latex     latex_user_state
internal/flow      voucher files named by FLOW_VOUCHER_DIR
```

The CLI opens one SQLite database for the session and the app's data. The flow
store is the only code that writes flow state and history. The auth service is
the boundary for user and group authorization. Apps access WhatsApp through the
runtime's `Chat` interface.
