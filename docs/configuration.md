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

## Settings and defaults

[`.env.example`](../.env.example) lists every setting with what it does and its
default. It is generated from the settings the bots read, so its defaults are
the ones the bots use. Do not edit it by hand: after adding or changing a
setting, run `mise run env:example`. The tests fail while the file is out of
date.

Settings are in three groups: `BOTKIT_*` settings every bot reads, `LATEX_*`
settings for the latex bot, and `FLOW_*` settings for the flow bot.

The flow bot has its own defaults for the `BOTKIT_RATE_LIMIT_*` settings because
one conversation can require several messages per minute. Where the bots differ,
`.env.example` leaves the value blank and names each bot's default. Setting a
`BOTKIT_RATE_LIMIT_*` variable overrides the bot's default.

The latex runner also caps either image dimension at 4096 pixels. That limit is
fixed in the renderer.

## Flow settings

The flow file is read once at startup. An unreadable or invalid file stops the
process instead of silently using the built-in example. The voucher directory is
created when the first voucher arrives. Voucher files are created with
mode 0600.
