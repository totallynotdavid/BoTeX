# Configuration

Copy `.env.example` to `.env` for local development. `mise run dev:flow`,
`mise run dev:latex`, and commands such as `mise exec -- bin/botkit-flow run`
load `.env`. A binary run directly, such as `bin/botkit-flow run`, does not read
`.env`; export its variables or use a service manager's environment file. Empty
values use the default.

The process reads and validates all selected values before opening SQLite or
WhatsApp. A malformed value reports its key and the process exits with status 1.
Durations use Go duration syntax such as `10s` and `5m`. Lists are
comma-separated. JID lists use `user@server` entries without device parts.

## Shared settings

| Variable                     | Default                           | Meaning                                                    |
| ---------------------------- | --------------------------------- | ---------------------------------------------------------- |
| `BOTKIT_STORE_PATH`          | `botkit.db`                       | SQLite file for the session and bot data.                  |
| `BOTKIT_LOG_LEVEL`           | `info`                            | `debug`, `info`, `warn`, or `error`.                       |
| `BOTKIT_OWNER_JIDS`          | empty                             | Comma-separated JIDs registered as owners at startup.      |
| `BOTKIT_RATE_LIMIT_REQUESTS` | `5` for `latex`, `20` for `flow`  | Requests per user per period.                              |
| `BOTKIT_RATE_LIMIT_PERIOD`   | `1m`                              | Window used by the rate limiter.                           |
| `BOTKIT_RATE_LIMIT_COOLDOWN` | `5m` for `latex`, `1m` for `flow` | Delay before another over-limit notice.                    |
| `BOTKIT_MAX_IN_FLIGHT`       | `10`                              | Messages handled concurrently. Extra messages are refused. |
| `BOTKIT_OWN_MESSAGES`        | `false`                           | Whether to handle messages sent by the bot's own account.  |
| `BOTKIT_ALLOW_ONLY`          | empty                             | Comma-separated sender JIDs. Empty accepts every sender.   |

The flow defaults to a higher request rate because one conversation can require
several messages per minute. Setting any `BOTKIT_RATE_LIMIT_*` variable
overrides the selected bot's default.

## LaTeX settings

| Variable                      | Default     | Meaning                                  |
| ----------------------------- | ----------- | ---------------------------------------- |
| `LATEX_MAX_LENGTH`            | `1000`      | Maximum LaTeX characters in one message. |
| `LATEX_MAX_IMAGE_BYTES`       | `5242880`   | Maximum PNG size sent to WhatsApp.       |
| `LATEX_TIMEOUT`               | `10s`       | Maximum wall time for a render.          |
| `LATEX_DATA_LIMIT_BYTES`      | `268435456` | Typst data-memory limit per render.      |
| `LATEX_FILE_SIZE_LIMIT_BYTES` | `16777216`  | Maximum file size Typst may write.       |

The runner also caps either image dimension at 4096 pixels. That limit is fixed
in the renderer.

## Flow settings

| Variable            | Default    | Meaning                                          |
| ------------------- | ---------- | ------------------------------------------------ |
| `FLOW_FILE`         | empty      | JSON flow file. Empty uses the built-in example. |
| `FLOW_VOUCHER_DIR`  | `vouchers` | Directory for payment-voucher images.            |
| `FLOW_TYPING_DELAY` | `0s`       | Delay before each reply.                         |

The flow file is read once at startup. An unreadable or invalid file stops the
process instead of silently using the built-in example. The voucher directory is
created when the first voucher arrives. Voucher files are created with
mode 0600.
