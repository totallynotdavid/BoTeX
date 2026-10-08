# Offline REPL

The REPL runs a bot in the terminal. It builds the real app over SQLite and an
in-memory transport, so it never connects to WhatsApp and needs no pairing. Rate
limiting, command routing, rendering, and stored state all apply.

```bash
mise exec -- bin/botkit-flow --repl
mise exec -- bin/botkit-latex --repl
```

`--repl` must be the first argument. Type a message at `you>`. The bot answers
at `bot>`. An image reply shows as `[image image/png, N bytes]`, a reaction as
`reaction ✅`, and a message the bot ignores as `(no reply)`. Type `:quit` or
`:exit`, or close stdin, to leave. Log lines go to stderr at `BOTKIT_LOG_LEVEL`,
so they can appear between the prompts.

The REPL reads the same environment as `run`, including `BOTKIT_STORE_PATH`, and
writes the bot's tables to that database. Point `BOTKIT_STORE_PATH` at a scratch
file to keep a session apart from a real one.

The flow bot starts Luma, the built-in flow. A path through it: `misteryo`,
`sí`, `recordatorios`, `sí`. The reaction ✅ follows the first `sí` and not the
reminder consent. That `sí` stores nothing: the choice was stored at `misteryo`,
and the route only carries the reaction. `qué elegí` shows the stored memory.
The REPL does not run the reminder scheduler.

## Rank

The REPL user is `51900000001@s.whatsapp.net`. The REPL registers it with the
first rank the bot defines besides `owner`: `user` for the latex bot. The flow
bot defines no such rank and checks no rank. Add `--owner` to run as an owner:

```bash
mise exec -- bin/botkit-latex --repl --owner
```

## What the REPL cannot check

The REPL replaces `internal/whatsapp`. It does not exercise pairing, the socket,
reconnection, event translation, or media upload and download. Those need a
paired account.
