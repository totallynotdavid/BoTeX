# Running

Pair the bot first ([pairing](pairing.md)). Then start it:

```bash
mise exec -- bin/botkit-flow run
mise exec -- bin/botkit-latex run
```

`run` is the default subcommand. `mise run dev:flow` and `mise run dev:latex`
run the source tree instead of the binary. A binary started without mise does
not read `.env`; see [configuration](configuration.md).

The latex bot runs `typst` and `prlimit` from `PATH`. `mise install` provides
`typst`. Install `prlimit` with the operating system's util-linux package.

Run one process for each `BOTKIT_STORE_PATH`. The flow bot's per-user lock lives
in the process, and WhatsApp ends a session that a second process connects with
the same keys.

## Shutdown and logs

The bot stops on SIGINT and SIGTERM. It cancels active handlers and waits up to
10 seconds for them to return. It reconnects on its own after a temporary
disconnect.

Logs are text records on stderr. `BOTKIT_LOG_LEVEL` sets their level.

## Service manager

This systemd unit runs the latex bot from a dedicated directory:

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

The default `BOTKIT_STORE_PATH` and `FLOW_VOUCHER_DIR` are relative, so they
land in `WorkingDirectory`. Make `typst` and `prlimit` available on the
service's `PATH`. A mise shim for `typst` fails with "config files are not
trusted" when the service runs with another `HOME`, so put the `typst` binary
itself on `PATH`. For the flow bot, change the environment file and the binary.

## Exit statuses

| Status | Meaning                                                                |
| ------ | ---------------------------------------------------------------------- |
| 0      | The process stopped after SIGINT or SIGTERM, or the command completed. |
| 1      | Configuration, database, connection, pairing, or application failure.  |
| 2      | An unknown subcommand, flag, or positional argument.                   |
| 78     | The WhatsApp session ended and needs the operator.                     |

Status 78 means the session ended for one of these reasons. The error log names
it and says what to do.

| Reason       | Cause                                      | Remedy                              |
| ------------ | ------------------------------------------ | ----------------------------------- |
| `not_paired` | The store holds no device.                 | Run `pair`.                         |
| `logged_out` | The device was unlinked from the account.  | Run `pair`.                         |
| `replaced`   | Another process uses this session.         | Stop the other process, then start. |
| `banned`     | WhatsApp banned the account for now.       | Start after the ban expires.        |
| `outdated`   | WhatsApp rejected this client version.     | Update the bot.                     |
| `refused`    | WhatsApp refused the connection otherwise. | Read the detail, then start again.  |

`RestartPreventExitStatus=78` keeps systemd from restarting a bot that only the
operator can fix.
