# Running

Pair the bot first ([pairing](pairing.md)). Then start it:

```bash
mise exec -- bin/botkit-flow run
mise exec -- bin/botkit-latex run
```

`run` is the default subcommand. `mise run dev:flow` and `mise run dev:latex`
run the source tree instead of the binary. A binary started without mise does
not read `.env`; see [configuration](configuration.md).

The latex bot runs `typst` and `prlimit`. It looks `typst` up on `PATH` unless
`LATEX_TYPST_BIN` names the binary ([configuration](configuration.md)), and it
exits at start when it cannot find it. `mise install` provides `typst`; the
[README](../readme.md#install) lists what else the bot needs. Install `prlimit`
with the operating system's util-linux package.

Run one bot process for each `BOTKIT_STORE_PATH`. The flow bot's per-user lock
lives in the process, and WhatsApp ends a session that a second process connects
with the same keys. The operator commands below are the exception: they run in a
second process, beside the running bot.

## Operator commands

`user`, `group`, and `handoff` change the bot's database while the bot runs. The
latex bot has `user` and `group` ([latex bot](latex-bot.md#access)); the flow
bot has `handoff` ([flow bot](flow-bot.md#fallbacks-and-hand-off)). Each prints
what it did and exits with status 0, 1 when it fails, or 2 for bad arguments.

They open an existing database only. When `BOTKIT_STORE_PATH`, or the default
`botkit.db` in the current directory, names no file, the command fails with
`store does not exist` and the path, and creates nothing. Run them in the bot's
working directory or with the bot's environment. For the shipped unit:

```bash
sudo -u botkit sh -c 'cd /var/lib/botkit-latex && set -a && . /etc/botkit/latex.env && botkit-latex user list'
```

## Shutdown and logs

The bot stops on SIGINT and SIGTERM. It cancels active handlers and waits up to
10 seconds for them to return. It reconnects on its own after a temporary
disconnect.

Logs are text records on stderr. `BOTKIT_LOG_LEVEL` sets their level.

## Service manager

Two tasks install the bots as services. As root, or with `PREFIX` and `UNIT_DIR`
set to writable directories:

```bash
mise run install:bin     # bin/botkit-* into $PREFIX/bin, default /usr/local/bin
mise run install:units   # contrib/systemd/*.service into $UNIT_DIR, default /etc/systemd/system
```

[`contrib/systemd`](../contrib/systemd) holds one unit for each bot. Each runs
`/usr/local/bin/botkit-<bot> run` as the `botkit` user, which must exist, with
`WorkingDirectory=/var/lib/botkit-<bot>` that systemd creates through
`StateDirectory=`, and `EnvironmentFile=/etc/botkit/<bot>.env`. Create the
environment file, then [pair](pairing.md) as the `botkit` user, from the working
directory and with the unit's environment file, so the session lands in the
database that the service opens. The shell inside the quotes loads the file,
because `sudo` does not pass your environment on. Then enable the unit:

```bash
sudo install -d -o botkit /var/lib/botkit-latex
sudo -u botkit sh -c 'cd /var/lib/botkit-latex && set -a && . /etc/botkit/latex.env && botkit-latex pair'
sudo systemctl enable --now botkit-latex
```

The default `BOTKIT_STORE_PATH` and `FLOW_VOUCHER_DIR` are relative, so they
land in `WorkingDirectory`. Make `prlimit` available on the service's `PATH`. A
mise shim for `typst` fails with "config files are not trusted" when the service
runs with another `HOME`, so set `LATEX_TYPST_BIN` in the environment file to
the path of the `typst` binary itself.

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
