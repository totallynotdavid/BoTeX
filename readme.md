# botkit

[![CodeQL](https://github.com/totallynotdavid/botkit/actions/workflows/codeql.yml/badge.svg)](https://github.com/totallynotdavid/botkit/actions/workflows/codeql.yml)
[![lint-and-testing](https://github.com/totallynotdavid/botkit/actions/workflows/golangci-lint.yml/badge.svg)](https://github.com/totallynotdavid/botkit/actions/workflows/golangci-lint.yml)
[![test](https://github.com/totallynotdavid/botkit/actions/workflows/test.yml/badge.svg)](https://github.com/totallynotdavid/botkit/actions/workflows/test.yml)

Two WhatsApp bots in Go, built on
[whatsmeow](https://github.com/tulir/whatsmeow) and sharing one runtime:

- **latex** renders LaTeX equations to images. Send `!latex \frac{a}{b}` in a
  chat or a registered group and the bot replies with a PNG. Access is
  rank-based, and every render runs under memory, time and size limits.
- **flow** walks each person who writes to it through a conversation you define
  in a JSON file: a menu, questions, a payment voucher, a hand-off to a person.

Each user of either bot is rate limited.

```bash
mise install && mise run build
bin/botkit-latex pair
bin/botkit-latex run
```

Use `bin/botkit-flow` in place of `bin/botkit-latex` to run the flow bot.

## Requirements

- Go and [typst](https://typst.app) 0.15, both installed by
  [mise](https://mise.jdx.dev/) (`mise install`). Only the latex bot needs
  typst.
- `prlimit` from util-linux, which applies the memory and file limits to typst.
  Linux only. Only the latex bot needs it.

The latex bot renders through the typst CLI and the
[mitex](https://typst.app/universe/package/mitex) package, which is embedded in
the binary. It never downloads packages.

## Pairing

A bot stores its WhatsApp session in the SQLite file `BOTKIT_STORE_PATH`. Link
it to an account once, in a terminal:

```bash
bin/botkit-latex pair                      # prints a QR code to scan
bin/botkit-latex pair --phone +51999999999 # prints a code to type on that phone
```

In WhatsApp, open Settings, Linked devices, Link a device. `pair` refuses to run
without a terminal on stdin and stdout, and when the store already holds a
device. Each bot needs its own `BOTKIT_STORE_PATH`.

`run` never prints a QR code. Started on an unpaired store, it stops with exit
status 78.

## Running

```bash
mise run dev:latex  # go run ./cmd/latex
mise run dev:flow   # go run ./cmd/flow
bin/botkit-latex run
bin/botkit-flow run
```

A bot stops cleanly on SIGINT and SIGTERM. Logs go to stderr, one text record
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
the service manager not to. A systemd unit for the latex bot:

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

typst and `prlimit` must be on the service's `PATH`. For the flow bot, change
the description, the environment file and the binary.

## Configuration

A bot reads environment variables, and `mise run dev:latex` and
`mise run dev:flow` load them from `.env`. Copy [.env.example](.env.example),
which lists every key with its default. A bad value stops the bot at start and
names each key that is wrong.

| Key                                                                                    | Default                                  | Sets                                              |
| -------------------------------------------------------------------------------------- | ---------------------------------------- | ------------------------------------------------- |
| `BOTKIT_STORE_PATH`                                                                    | `botkit.db`                              | SQLite file for the session, users and ranks      |
| `BOTKIT_LOG_LEVEL`                                                                     | `info`                                   | `debug`, `info`, `warn` or `error`                |
| `BOTKIT_OWNER_JIDS`                                                                    | none                                     | JIDs registered as owners on every start          |
| `BOTKIT_RATE_LIMIT_REQUESTS`, `BOTKIT_RATE_LIMIT_PERIOD`, `BOTKIT_RATE_LIMIT_COOLDOWN` | latex 5, `1m`, `5m`; flow 20, `1m`, `1m` | Requests per user per period, and notice cooldown |
| `BOTKIT_MAX_IN_FLIGHT`                                                                 | 10                                       | Messages handled at once                          |
| `BOTKIT_OWN_MESSAGES`                                                                  | `false`                                  | Answer messages from the bot's own account        |
| `BOTKIT_ALLOW_ONLY`                                                                    | none                                     | Answer only these JIDs. Empty answers everyone    |
| `LATEX_MAX_LENGTH`                                                                     | 1000                                     | Characters of LaTeX per message                   |
| `LATEX_MAX_IMAGE_BYTES`                                                                | 5242880                                  | Largest PNG sent                                  |
| `LATEX_TIMEOUT`                                                                        | `10s`                                    | Longest a render runs                             |
| `LATEX_DATA_LIMIT_BYTES`, `LATEX_FILE_SIZE_LIMIT_BYTES`                                | 268435456, 16777216                      | typst's data memory and largest written file      |
| `FLOW_FILE`                                                                            | none                                     | The flow the bot runs. Empty runs the example     |
| `FLOW_VOUCHER_DIR`                                                                     | `vouchers`                               | Where payment vouchers are saved                  |
| `FLOW_TYPING_DELAY`                                                                    | `0s`                                     | Wait before each reply                            |

The flow bot's limit is higher because a customer walking a menu sends several
messages a minute. Setting a `BOTKIT_RATE_LIMIT_*` key overrides the default of
either bot.

## The latex bot

### Access

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

### What it renders

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

## The flow bot

The flow bot answers direct messages from anyone: it has no ranks and ignores
groups. It keeps where each user is in the conversation, and what they have told
it, in the store, so a conversation continues after a restart. A user who has
been silent for a day is greeted again and starts from the first node.

```bash
bin/botkit-flow pair
bin/botkit-flow run
```

With no `FLOW_FILE` it runs the example flow built into the binary. The `FLOW_*`
keys are in the [configuration](#configuration) table:

- `FLOW_FILE` is the path of the flow to run. A file that cannot be read, or
  that is not a valid flow, stops the bot at start and lists each problem, so a
  typo never serves the example to real users. The bot reads the file once, so
  restart it after an edit.
- `FLOW_VOUCHER_DIR` is where the bot saves the images users send as payment
  vouchers, one file each, readable only by the bot's user. The directory is
  created on the first voucher.
- `FLOW_TYPING_DELAY` makes the bot wait before each reply, so an answer does
  not land the instant the user writes.

A few messages the bot words itself, in Spanish: the greeting, the note before a
repeated message, and the replies to a file of a kind the node does not take, to
a name it cannot use and to an action that failed. Every other message comes
from the flow.

### The example flow

[example.json](internal/flow/fsm/example.json) is a small bookshop's menu in
Spanish: prices, schedules, enrolling in a reading club and paying with a photo
of a voucher. It uses every feature below. Copy it, edit the copy and point
`FLOW_FILE` at it.

### Writing a flow

A flow is a JSON file. [flow.go](internal/flow/fsm/flow.go) defines every field
and [load.go](internal/flow/fsm/load.go) the checks made when it loads. This
flow asks for `1`, answers it, and sends anyone who writes `help` to a person:

```json
{
  "start_node": "WELCOME",
  "nodes": {
    "WELCOME": {
      "message": {
        "type": "text",
        "content": "Hi {{name}}! Write *1* for our prices, or *help*."
      },
      "transitions": [
        {
          "condition": { "type": "exact", "value": ["1"] },
          "target": "PRICES"
        }
      ]
    },
    "PRICES": {
      "message": { "type": "text", "content": "Everything costs 10." }
    },
    "NEEDS_ASSISTANCE": {
      "message": { "type": "text", "content": "A person will write to you." },
      "action": "escalate_to_human_agent"
    }
  },
  "global_transitions": [
    {
      "condition": { "type": "keyword", "value": ["help"] },
      "target": "NEEDS_ASSISTANCE"
    }
  ]
}
```

**Nodes.** A node is one step of the conversation. When a user enters it the bot
sends its `message.content`, which may use WhatsApp formatting such as `*bold*`.
`start_node` is where a new user begins. Every flow needs a node named
`NEEDS_ASSISTANCE`, where users go to reach a person.

**Transitions.** A node's `transitions` say where a message leads. The bot tries
them in order and takes the first whose `condition` matches, moving the user to
its `target`. A transition may carry an `action`.

**Conditions.** The text of a message, or the caption of a file, is compared
like this:

| `type`       | Matches                                                                                                                                                    |
| ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `exact`      | The whole text equals one of `value`, ignoring case and surrounding spaces.                                                                                |
| `keyword`    | The text has one of `value` as whole words, ignoring case and accents. A one-word value longer than four letters also matches a word with up to two typos. |
| `regex`      | The text matches `regex`, ignoring case. The syntax is Go's.                                                                                               |
| `any_text`   | Any text sent without a file.                                                                                                                              |
| `media`      | Any file.                                                                                                                                                  |
| `media_type` | A file of one of the kinds in `value`: `image`, `video`, `audio`, `document` or `sticker`.                                                                 |

**Includes.** `transition_groups` holds named lists of transitions. A node with
`"include_transitions": "menu"` tries the list `menu` before its own
transitions, so a menu written once serves many nodes. One node includes one
list.

**Global transitions.** `global_transitions` apply from every node, after the
node's own. A node with `"ignore_global_transitions": true` skips them, except
those that lead to `NEEDS_ASSISTANCE`, so a user is never stuck without a way to
ask for help.

**Fallbacks.** When nothing matches, the bot repeats the node's message after a
short note, or sends the node's `fallback_message` alone when it has one. A node
with no transitions at all ends the conversation and stays silent. A node that
waits for a file and gets another kind says which kinds it takes. After three
misses in a row the user goes to `NEEDS_ASSISTANCE`.

**Escalation.** A user reaches `NEEDS_ASSISTANCE` through a transition, after
three misses in a row, or when an action fails. The last two mark the user with
the action `escalate_to_human_agent` themselves, and a node action does it for
the first, so give `NEEDS_ASSISTANCE` that action. It sets
`requires_human_agent` in the store's `user_state` table. The bot sends nobody a
notice, so a person has to read that column.

**Actions.** A transition's `action` runs as the user takes it, and a node's
`action` runs whenever the user enters it. The names are fixed, and the bot
refuses to start when a flow uses another:

| Action                          | Does                                                                         |
| ------------------------------- | ---------------------------------------------------------------------------- |
| `create_new_lead`               | Nothing to the store. It marks where a new user starts.                      |
| `save_user_name`                | Stores the text as the user's name, without an introduction like "me llamo". |
| `clear_user_name`               | Forgets the name.                                                            |
| `set_selected_course`           | Remembers the node the action came from, for `{{course_name}}`.              |
| `update_lead_interest_beginner` | Records the interest `beginner`.                                             |
| `update_lead_interest_advanced` | Records the interest `advanced`.                                             |
| `update_lead_consulted_price`   | Records that the user asked for prices.                                      |
| `save_payment_voucher`          | Saves the image sent as a voucher. Any other message escalates.              |
| `escalate_to_human_agent`       | Marks the user as needing a person.                                          |

[actions.go](internal/flow/actions.go) lists them and what each stores.

**Templates.** A message may use these, replaced when it is sent:

| Template          | Becomes                                                                                     |
| ----------------- | ------------------------------------------------------------------------------------------- |
| `{{name}}`        | The user's first name, or `amigx` when the bot has none.                                    |
| `{{greeting}}`    | A welcome for a new user, and a welcome back for one who has talked to the bot before.      |
| `{{course_name}}` | The `title` of the node `set_selected_course` remembered. Use it after that action has run. |

A `{{...}}` the bot does not know is sent as written.

## Development

```bash
mise run test        # go test -race ./...
mise run lint        # golangci-lint, fixing what it can
mise run lint:check  # the same checks, changing nothing
mise run ci          # build, lint:check and test, as CI does
mise run build       # bin/botkit-latex and bin/botkit-flow
```

The tests run the real typst binary, so it must be on `PATH`. No test connects
to WhatsApp: they use a fake client, and the flow's conversation tests run whole
scripts of messages against the example flow through the flow binary's own
wiring. [ARCHITECTURE.md](ARCHITECTURE.md) maps the code.

---

Built with [whatsmeow](https://github.com/tulir/whatsmeow), inspired by
[matterbridge](https://github.com/42wim/matterbridge).
