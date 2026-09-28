# botkit

[![CodeQL](https://github.com/totallynotdavid/botkit/actions/workflows/codeql.yml/badge.svg)](https://github.com/totallynotdavid/botkit/actions/workflows/codeql.yml)
[![lint-and-testing](https://github.com/totallynotdavid/botkit/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/totallynotdavid/botkit/actions/workflows/golangci-lint.yml)
[![test](https://github.com/totallynotdavid/botkit/actions/workflows/test.yml/badge.svg)](https://github.com/totallynotdavid/botkit/actions/workflows/test.yml)

A WhatsApp bot that renders LaTeX equations to images, built in Go on
[whatsmeow](https://github.com/tulir/whatsmeow). Send `!latex \frac{a}{b}` in a
chat or a registered group and the bot replies with a PNG. Access is rank-based,
each user is rate limited, and every render runs under memory, time and size
limits.

```bash
mise install && mise run build
bin/botkit-latex pair
bin/botkit-latex run
```

## Requirements

- Go and [typst](https://typst.app) 0.15, both installed by
  [mise](https://mise.jdx.dev/) (`mise install`).
- `prlimit` from util-linux, which applies the memory and file limits to typst.
  Linux only.

The bot renders through the typst CLI and the
[mitex](https://typst.app/universe/package/mitex) package, which is embedded in
the binary. It never downloads packages.

## Pairing

The bot stores its WhatsApp session in the SQLite file `BOTKIT_STORE_PATH`. Link
it to an account once, in a terminal:

```bash
bin/botkit-latex pair                      # prints a QR code to scan
bin/botkit-latex pair --phone +51999999999 # prints a code to type on that phone
```

In WhatsApp, open Settings, Linked devices, Link a device. `pair` refuses to run
without a terminal on stdin and stdout, and when the store already holds a
device.

`run` never prints a QR code. Started on an unpaired store, it stops with exit
status 78.

## Running

```bash
mise run dev        # go run ./cmd/latex
bin/botkit-latex run
```

The bot stops cleanly on SIGINT and SIGTERM. Logs go to stderr, one text record
per line, at `BOTKIT_LOG_LEVEL`.

### Exit status

| Status | Meaning                                                                                              |
| ------ | ---------------------------------------------------------------------------------------------------- |
| 0      | Stopped by SIGINT or SIGTERM.                                                                        |
| 1      | Any other failure: bad configuration, an unreadable store, a connection error.                       |
| 2      | Bad command line.                                                                                    |
| 78     | The WhatsApp session ended: not paired, logged out, replaced by another process, banned or outdated. |

On 78 the bot logs one ERROR record with the reason and the fix. A logged-out
device needs `pair` again. A replaced session means another process uses the
same account, and one of the two must stop. Restarting does not help, so tell
the service manager not to. A systemd unit:

```ini
[Unit]
Description=botkit latex bot
After=network-online.target
Wants=network-online.target

[Service]
User=botkit
WorkingDirectory=/var/lib/botkit
EnvironmentFile=/etc/botkit/latex.env
ExecStart=/usr/local/bin/botkit-latex run
Restart=on-failure
RestartPreventExitStatus=78

[Install]
WantedBy=multi-user.target
```

typst and `prlimit` must be on the service's `PATH`.

## Configuration

The bot reads environment variables, and `mise run dev` loads them from `.env`.
Copy [.env.example](.env.example), which lists every key with its default. A bad
value stops the bot at start and names each key that is wrong.

| Key                                                                                    | Default             | Sets                                              |
| -------------------------------------------------------------------------------------- | ------------------- | ------------------------------------------------- |
| `BOTKIT_STORE_PATH`                                                                    | `botkit.db`         | SQLite file for the session, users and ranks      |
| `BOTKIT_LOG_LEVEL`                                                                     | `info`              | `debug`, `info`, `warn` or `error`                |
| `BOTKIT_OWNER_JIDS`                                                                    | none                | JIDs registered as owners on every start          |
| `BOTKIT_RATE_LIMIT_REQUESTS`, `BOTKIT_RATE_LIMIT_PERIOD`, `BOTKIT_RATE_LIMIT_COOLDOWN` | 5, `1m`, `5m`       | Requests per user per period, and notice cooldown |
| `BOTKIT_MAX_IN_FLIGHT`                                                                 | 10                  | Messages handled at once                          |
| `BOTKIT_OWN_MESSAGES`                                                                  | `false`             | Answer messages from the bot's own account        |
| `LATEX_MAX_LENGTH`                                                                     | 1000                | Characters of LaTeX per message                   |
| `LATEX_MAX_IMAGE_BYTES`                                                                | 5242880             | Largest PNG sent                                  |
| `LATEX_TIMEOUT`                                                                        | `10s`               | Longest a render runs                             |
| `LATEX_DATA_LIMIT_BYTES`, `LATEX_FILE_SIZE_LIMIT_BYTES`                                | 268435456, 16777216 | typst's data memory and largest written file      |

## Access

A user needs a rank to run any command. The `owner` rank runs everything, and
`BOTKIT_OWNER_JIDS` registers its holders on every start without touching a user
who already has another rank. The `user` rank runs `help` and `latex`.

There is no chat command to register users or groups yet. Insert them into the
store by hand. Users are keyed by phone-number JID, groups by group JID, and a
group's members still need a rank of their own:

```bash
sqlite3 botkit.db "INSERT INTO users (user_id, rank, registered_by) VALUES ('51999999999@s.whatsapp.net', 'user', 'operator')"
sqlite3 botkit.db "INSERT INTO registered_groups (group_id, registered_by) VALUES ('120363000000000000@g.us', 'operator')"
```

Direct chats need no group registration. The ranks and the checks are described
in [internal/auth](internal/auth/doc.go).

## What it renders

The whole message after `!latex` is one math formula, rendered by mitex. It
handles the amsmath-style math most people type: fractions, roots, sums,
integrals, matrices, `align`, `\text{...}` and `\textbf{...}`. The `physics`
macros `\abs`, `\norm`, `\dv` and `\pdv` work.

What LaTeX has beyond that does not:

- Document structure and packages: `\documentclass`, `\usepackage` and
  `\begin{document}`.
- `mhchem` (`\ce`) and the `physics` macro `\qty`.
- Any other command mitex does not define.

The bot answers those with the reason, for example `unknown command: \ce`, and
reacts ❌.

## Development

```bash
mise run test       # go test -race ./...
mise run lint       # golangci-lint
```

The tests run the real typst binary, so it must be on `PATH`. No test connects
to WhatsApp. [ARCHITECTURE.md](ARCHITECTURE.md) maps the code.

---

Built with [whatsmeow](https://github.com/tulir/whatsmeow), inspired by
[matterbridge](https://github.com/42wim/matterbridge).
