# Operations

## Install and build

Install the versions selected by `mise.toml`, then build both binaries:

```bash
mise install
mise run build
```

The build writes `bin/botkit-flow` and `bin/botkit-latex`. `mise install` also
installs the `sqlite3` CLI for access administration. The LaTeX bot needs
`typst` 0.15 and `prlimit` from util-linux; install `prlimit` with the operating
system package manager when it is absent.

## Pair an account

Each bot has its own WhatsApp session in `BOTKIT_STORE_PATH`. Set a different
path before pairing two bots.

Pair with a QR code:

```bash
mise exec -- bin/botkit-flow pair
```

Or request a pairing code for a phone number:

```bash
mise exec -- bin/botkit-flow pair --phone +51999999999
```

Replace `botkit-flow` with `botkit-latex` for the other bot. Open WhatsApp on
the phone, choose Settings, Linked devices, and Link a device. Scan the QR code
or enter the printed pairing code. Pairing refuses when stdin or stdout is not a
terminal, or when the store already contains a device.

Pairing is the only command that displays a QR code or pairing code. `run` does
not pair an account. If its store has no device, it exits with status 78.

## Run a bot

The default subcommand is `run`:

```bash
mise exec -- bin/botkit-flow run
mise exec -- bin/botkit-latex run
```

`mise run dev:flow` and `mise run dev:latex` run the corresponding source tree
directly. They load `.env` through mise. A production service should load the
same settings from its service manager and run one process per account.

The runtime reconnects after a temporary disconnect. It stops on SIGINT and
SIGTERM and waits briefly for active handlers. Logs are text records on stderr.
`BOTKIT_LOG_LEVEL` controls their level.

## Service manager

This systemd unit runs the LaTeX bot from a dedicated directory:

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

Make `typst` and `prlimit` available on the service's `PATH`. Change the
environment file and binary for the flow bot. Keep one process on each
`BOTKIT_STORE_PATH`; flow's per-user lock does not cross processes.

## Exit statuses

| Status | Meaning                                                                |
| ------ | ---------------------------------------------------------------------- |
| 0      | The process stopped after SIGINT or SIGTERM, or the command completed. |
| 1      | Configuration, database, connection, pairing, or application failure.  |
| 2      | An unknown subcommand, flag, or positional argument.                   |
| 78     | The WhatsApp session ended and needs operator action.                  |

Status 78 covers an unpaired or logged-out store, a session replaced by another
process, a ban, an outdated client, or another refused session. The error log
names the reason. Pair again after a logout or unpaired store. Stop the other
process after a replacement; restarting both processes does not resolve it.
