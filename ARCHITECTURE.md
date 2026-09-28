# Architecture

## The bot runtime (`internal/bot`)

`Bot.Run` receives events on the client's connection goroutine and hands each
accepted message to the app on its own handler goroutine. That makes a handful
of fields shared between goroutines. They all live on `Bot`.

### Shared state

| State           | Written by                                             | Read by                                          | Guard                       |
| --------------- | ------------------------------------------------------ | ------------------------------------------------ | --------------------------- |
| `started`       | `Run`, once                                            | `Run`                                            | atomic                      |
| `ctx`, `cancel` | `Run`, before `Connect`; cancelled by `end` or `Run`   | `Connect`, handler goroutines, `onEvent`         | written before events start |
| `stopped`       | `stop` (from `Run`) and `end`                          | `spawn`                                          | `mu`                        |
| `ended`         | `end`, first call only                                 | `Run`, every `Chat` method, through `Chat.ended` | `mu`                        |
| `handlers`      | `spawn` adds, handler goroutine calls `Done`           | `Run` waits                                      | `Add` under `mu`            |
| `slots`         | connection goroutine takes, handler goroutine releases | both                                             | channel                     |

`slots` holds one token per running `Handle`. The connection goroutine takes a
token without blocking and drops the message as `busy` when none is free. The
handler releases its token when `Handle` returns. If `spawn` refuses because the
runtime stopped, the connection goroutine releases it instead.

### States

```
new --Run--> connecting --Connect ok--> running --ctx done--> stopping --> returned
                 |                                               ^
                 +--Connect fails--------------------------------+
```

`Run` has one lifetime context, `ctx`, derived from the caller's with
`context.WithCancelCause`. It is done when the caller cancels or when `end`
cancels it with the `*SessionEndedError` as cause. `Connect`, the wait in
`running` and every handler use it, so a caller cancel and a session end abort a
blocked dial and a running bot the same way.

- **connecting**: `Connect` is running. Events may already arrive, so a session
  end can happen here.
- **running**: `Connect` returned nil. `Run` waits for `ctx` to be done.
  Messages become handlers.
- **stopping**: `stopped` is true. No handler starts. `Run` waits up to the
  grace period for the ones already running, then disconnects if it connected.
- **returned**: `Run` has returned. A `Bot` runs once, so there is no way back.

`stopped` goes from false to true once and never back. `ended` goes from nil to
one `*SessionEndedError` once; later session ends are ignored. A
`*SessionEndedError` returned by `Connect` (`NotPaired`) is recorded through
`end` too, so it takes the same path.

### One exit rule

Every way `Run` can end passes through `stopping` and then one function,
`result`, which decides the return value in this order:

1. A recorded session end: report it (one `session_ended` record and one ERROR
   log line) and return it.
2. Otherwise, if the caller's context is done, return its error.
3. Otherwise the error came from `Connect`: return it wrapped as `connect: %w`.

### Invariants

1. `mu` guards `stopped`, `ended` and every `handlers.Add`. `Add` happens only
   while `stopped` is false, and `stop` and `end` set `stopped` before `Run`
   calls `Wait`. `Wait` therefore sees a fixed set of handlers.
2. `end` sets `ended` before it cancels `ctx` with that error as the cause, all
   under `mu`. Anything that sees the context cancelled with a
   `*SessionEndedError` cause also sees `ended` set.
3. `mu` is never held while calling the client, the app or the `Recorder`, so
   none of them can deadlock against the runtime.
4. A session end that has happened by the time `Run` reaches `result` is
   reported and returned, even when the caller's context was cancelled at the
   same moment.

## The command router (`internal/command`)

`Router.Handle` asks `Permissions.Authorize(user, group, command)` once, where
`group` is the chat of a group message and empty for a direct chat.
`auth.Service` answers with a `Decision`, checking in this order: the user is
registered, the user's rank lists the command, the group is registered and
active. The first failure names the reason in the 🚫 notice. Direct chats need
no registration. A group message with no group ID is an error, so a missing ID
cannot skip the group check.

## The conversation flow (`internal/flow`)

The flow keeps what each user has told the bot, so a conversation continues
where it stopped. `internal/flow/fsm` decides where a message leads. `flow`
holds the state, applies the actions a transition names, and stores the result.

### Who owns what

`Store` is the only code that reads or writes the `user_state` and
`conversation_history` tables, and `Store.Turn` is the only way to change a
user's `State`. Nothing else in the process holds a `State` between messages: a
handler gets one inside `Turn` and hands it back when `Turn` returns.

### One transition path

`Turn(ctx, user, turn)` runs in this order:

1. It takes the user's lock, waiting for the turn that holds it. A cancelled
   `ctx` ends the wait with an error and `turn` never runs.
2. It loads the user's state. A user with no row gets a fresh state with only
   `UserID` set, so `CurrentNode` is empty.
3. It calls `turn` with that state. `turn` applies actions, sends replies and
   returns the messages of the turn for the history.
4. If `turn` returned nil, it stamps `LastUpdated` with the current time in UTC
   and writes the state and the messages in one transaction. Either both are
   stored or neither is.
5. It releases the lock.

If `turn` or the write fails, nothing is stored and the error comes back
wrapped. The next turn loads what the last successful one saved.

### Concurrency

`bot.Run` runs each message on its own goroutine, so two messages of one user
can arrive together. The per-user lock makes their turns run one at a time, so
neither reads a state the other is about to replace, and replies sent from
`turn` keep the order of the turns. The lock is in memory, in a map of one entry
for each user with a turn running or waiting. An entry is removed when its last
turn ends, so the map does not grow with the number of users.

Turns of different users share no lock and run in parallel. SQLite serializes
their writes, and `internal/sqlite` sets a busy timeout for that wait.

Two processes on one database are not supported. The lock does not cross
processes, so their turns for one user could overwrite each other.

### Actions

`Actions` holds the vocabulary a flow file may name, in one table. `Apply` runs
from it and `Check` reads the same table, so what a flow can name is what runs.
`Check` reports each unknown action with its place in the flow. It is meant to
run when the flow loads, so a mistyped action fails at startup and not when a
user first reaches it. Actions change the `State` or return an error and log
nothing. Vouchers are saved in a private file, mode 0600, because they show
payment details.

## The latex command (`internal/latex`)

`Run` refuses input over `MaxLength` characters, then renders one typst document
through `typst.Runner`: `#mitex(read("input.tex"))`, where `input.tex` is the
fixed physics-macro preamble followed by the user's code. mitex 0.2.7 is
embedded and extracted once by `New` to a private directory that typst reads as
its package path; `Close` removes it.

A render fails in one of four ways, and each is answered with a short text and
returned, so the router reacts ❌:

| Cause                                           | Reply                               |
| ----------------------------------------------- | ----------------------------------- |
| `typst.ErrLimit` (deadline, kill signal, OOM)   | took too long or too many resources |
| mitex's WASM trap (`unreachable`)               | the same                            |
| `typst.ErrTooLarge` (pixels or bytes)           | too large to send                   |
| `*typst.RenderError` (a mitex or typst message) | the first line of the message       |

A macro that expands forever (`\newcommand{\x}{\x\x}\x`) allocates until mitex's
WASM heap cannot grow, which the data limit turns into the trap, so the trap is
reported as a limit. Macros that call each other expand without allocating and
end at the deadline.

## The latex binary (`cmd/latex`)

`main` parses the subcommand (`run`, the default, or `pair`), starts a context
that SIGINT and SIGTERM cancel, and exits with the status `execute` returns. It
holds no logic of its own.

`run` in `cmd/latex/run.go` is the wiring, and it takes its settings and a
function that opens the WhatsApp client, so tests drive it with `whatsapp/fake`.
In order it:

1. opens the SQLite store (`internal/sqlite`);
2. creates the auth tables with the `user` rank and seeds the owners;
3. builds the latex command (`internal/latex`) and the rate limiter;
4. opens the client, which loads the session from the same store;
5. builds `command.NewRouter("!", auth, latex)` and runs `bot.New(...).Run` with
   `Options.Groups` on.

Settings come from `internal/config.Env`: the `BOTKIT_*` keys are read by
`Env.Shared` and the `LATEX_*` keys by `latex.ConfigFromEnv`. `Env` records
every malformed value and `Err` reports them together, so a bad `.env` fails
startup once, naming each key.

### Exit status

`exitStatus` maps what `run` returned:

| Returned                                                  | Status |
| --------------------------------------------------------- | ------ |
| nil, or the context error after a signal cancelled it     | 0      |
| a `*bot.SessionEndedError`, wherever it sits in the chain | 78     |
| anything else                                             | 1      |

`Bot.Run` returns the `*SessionEndedError` even when the caller's context was
cancelled at the same moment, so a session end is never mistaken for a signal.
Status 78 is `EX_CONFIG`: the operator has to act (pair again, or stop a second
process), so the readme's systemd unit lists it in `RestartPreventExitStatus`.
`main` maps a bad command line to 2.

## Pairing (`internal/whatsapp`)

`Pair` is the only code that shows a QR code or a pairing code, and only
`cmd/latex pair` calls it. It refuses before opening the store when the phone
number is malformed or stdin or stdout is not a terminal, and before dialling
when the store already holds a device. Otherwise it listens on whatsmeow's QR
channel, prints each code, and returns when the phone confirms, the codes expire
or WhatsApp rejects the pairing.
