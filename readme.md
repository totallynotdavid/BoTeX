# botkit

botkit is two self-hosted WhatsApp bots written in Go, for people who run a bot
on their own WhatsApp account. A bot links to the account as a linked device
through [whatsmeow](https://github.com/tulir/whatsmeow). It does not use the
WhatsApp Business API. Each bot keeps its WhatsApp session and its data in one
SQLite file.

- **flow** walks everyone who messages it through a conversation that you write
  as a JSON file.
- **latex** answers `!latex <equation>` with the equation rendered as a PNG. It
  serves registered users and groups.

The latex bot needs Linux and `prlimit` from util-linux.

## Install

```bash
git clone https://github.com/totallynotdavid/botkit
cd botkit
mise install
mise run build
```

The build writes `bin/botkit-flow` and `bin/botkit-latex`. `mise install`
installs Go, Typst, and the other tools pinned in [`mise.toml`](mise.toml).

## Try a bot

The offline REPL needs no WhatsApp account. Type a message at `you>`, read the
reply at `bot>`, and type `:quit` to leave.

```bash
mise exec -- bin/botkit-latex --repl
```

```text
you> !latex \frac{a}{b}
bot> [image image/png, 2486 bytes]
bot> reaction ✅
```

`mise exec -- bin/botkit-flow --repl` starts the flow bot. Type `hola` and it
answers as Luma, its built-in Spanish reading-club guide.

## Run a bot on WhatsApp

```bash
mise exec -- bin/botkit-flow pair
mise exec -- bin/botkit-flow run
```

`pair` shows a QR code to scan in WhatsApp under Linked devices. `run` answers
messages until SIGINT or SIGTERM.

## Features

- Pairing with a QR code or a pairing code.
- Flow: a conversation graph in JSON, with exact, keyword, regex, and media
  conditions. A keyword tolerates typos.
- Flow: per-user state and message history in SQLite, payment images saved to
  disk, a flag for users who need a person, and reminders for users who opt in.
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
