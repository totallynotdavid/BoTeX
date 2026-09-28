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
