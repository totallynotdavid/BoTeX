# Configuration

Both bots read their settings from environment variables. An empty value uses
the default.

Copy [`.env.example`](../.env.example) to `.env` for local use.
`mise run dev:flow`, `mise run dev:latex`, and
`mise exec -- bin/botkit-flow run` load `.env`. A binary run directly does not
read it; export the variables or point a service manager's `EnvironmentFile=` at
the file.

The process reads and validates every setting before it opens SQLite or
WhatsApp. A malformed value names its key, and the process exits with status 1.

| Type     | Syntax                                                        |
| -------- | ------------------------------------------------------------- |
| Duration | Go syntax, such as `10s` or `5m`.                             |
| JID list | Comma-separated `user@server` entries, without a device part. |
| Bytes    | A plain base-10 integer.                                      |
| Boolean  | `1`, `t`, `true`, `0`, `f`, or `false`, in any case.          |
| Level    | `debug`, `info`, `warn`, or `error`, in any case.             |

## Shared by every bot

| Variable                     | Default               | Meaning                                                                                                |
| ---------------------------- | --------------------- | ------------------------------------------------------------------------------------------------------ |
| `BOTKIT_STORE_PATH`          | `botkit.db`           | SQLite file holding the WhatsApp session, users, ranks, and bot state.                                 |
| `BOTKIT_LOG_LEVEL`           | `info`                | Level of the logs on stderr.                                                                           |
| `BOTKIT_OWNER_JIDS`          | none                  | JIDs registered as owners on every start. Without one, nobody can run a command until a user is added. |
| `BOTKIT_RATE_LIMIT_REQUESTS` | latex `5`, flow `20`  | Messages each user may send per period, at least 1.                                                    |
| `BOTKIT_RATE_LIMIT_PERIOD`   | `1m`                  | How long a message counts against the limit, at least 1ms.                                             |
| `BOTKIT_RATE_LIMIT_COOLDOWN` | latex `5m`, flow `1m` | Least time between two notices to a user who stays over the limit.                                     |
| `BOTKIT_MAX_IN_FLIGHT`       | `10`                  | Messages handled at once, at least 1. Beyond it the bot reacts ⚠️ and sends a busy notice.             |
| `BOTKIT_OWN_MESSAGES`        | `false`               | Answer messages sent from the bot's own WhatsApp account.                                              |
| `BOTKIT_ALLOW_ONLY`          | none                  | JIDs the bot answers. Everyone else is ignored. Empty answers everyone.                                |

A user over the rate limit gets a ⚠️ reaction, and a text notice at most once
per cooldown.

## Latex bot

| Variable                      | Default               | Meaning                                              |
| ----------------------------- | --------------------- | ---------------------------------------------------- |
| `LATEX_MAX_LENGTH`            | `1000`                | Most characters of LaTeX in one message, at least 1. |
| `LATEX_MAX_IMAGE_BYTES`       | `5242880` (5 MiB)     | Largest PNG the bot sends.                           |
| `LATEX_TIMEOUT`               | `10s`                 | Longest a render may run, at least 1ms.              |
| `LATEX_DATA_LIMIT_BYTES`      | `268435456` (256 MiB) | Data memory the `typst` process may use.             |
| `LATEX_FILE_SIZE_LIMIT_BYTES` | `16777216` (16 MiB)   | Largest file the `typst` process may write.          |

Each image side is also capped at 4096 pixels. The cap is fixed.

## Flow bot

| Variable            | Default    | Meaning                                                                   |
| ------------------- | ---------- | ------------------------------------------------------------------------- |
| `FLOW_FILE`         | none       | Path of the flow JSON file. Empty runs the example built into the binary. |
| `FLOW_VOUCHER_DIR`  | `vouchers` | Directory for payment vouchers. Created when the first one arrives.       |
| `FLOW_TYPING_DELAY` | `0s`       | How long the bot waits before each reply. `0s` replies at once.           |

The bot reads the flow file once, at startup. A file that cannot be read or is
not a valid flow stops the bot instead of falling back to the example.
