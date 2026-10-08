# botkit

botkit is two self-hosted WhatsApp bots written in Go. Each bot keeps its
WhatsApp session and its data in one SQLite file.

- **flow** walks everyone who messages it through a conversation that you write
  as a JSON file.
- **latex** answers `!latex <equation>` with the equation rendered as a PNG. It
  serves registered users and groups.

The latex bot needs Linux, `typst`, and `prlimit` from util-linux.

## Install

```bash
git clone https://github.com/totallynotdavid/botkit
cd botkit
mise install
mise run build
```

The build writes `bin/botkit-flow` and `bin/botkit-latex`.

## Try a bot

The offline REPL needs no WhatsApp account. Type a message at `you>`, read the
reply at `bot>`, and type `:quit` to leave.

```bash
mise exec -- bin/botkit-flow --repl     # type: hola
mise exec -- bin/botkit-latex --repl    # type: !latex \frac{a}{b}
```

The flow bot greets you with its built-in bookshop flow. The latex bot replies
`bot> [image image/png, ...]` and reacts with ✅.

## Run a bot on WhatsApp

```bash
mise exec -- bin/botkit-flow pair
mise exec -- bin/botkit-flow run
```

`pair` shows a QR code to scan in WhatsApp under Linked devices. `run` answers
messages until SIGINT or SIGTERM.

## Features

- Pair with a QR code or a pairing code.
- Flow: keyword, exact, regex, and media conditions, with typo-tolerant
  keywords.
- Flow: per-user state and conversation history in SQLite, payment-voucher
  images saved to disk, and a flag for users who need a person.
- Latex: Typst with the Mitex package, under limits on time, memory, file size,
  and image size.
- Latex: ranks, owners, and registered groups decide who can run a command.
- A per-user rate limit and a cap on messages handled at once.
- Exit status 78 when the WhatsApp session needs the operator, so a service
  manager can stop restarting the bot.
- An offline REPL that runs the real bot over an in-memory transport.

## More

The manual is in [docs/readme.md](docs/readme.md). To change botkit, read
[.github/contributing.md](.github/contributing.md).
