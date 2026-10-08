# The latex bot

The latex bot renders `!latex <equation>` as a PNG. It answers in direct chats
and in groups. It needs `typst` and `prlimit` on `PATH` ([running](running.md)).

## Access

Every command needs a registered user. A command sent in a group also needs the
group to be registered. The router checks, in order, that the user exists, that
the user's rank lists the command, and that the group is registered. A refusal
gets a 🚫 reaction and a reply that names the reason.

| Rank    | Commands        |
| ------- | --------------- |
| `user`  | `help`, `latex` |
| `owner` | every command   |

Set `BOTKIT_OWNER_JIDS` to register owners on every start:

```dotenv
BOTKIT_OWNER_JIDS=51999999999@s.whatsapp.net
```

The seed creates a missing owner. It leaves a user who already has another rank,
or who is deactivated, unchanged, and logs a warning.

Register other users and groups in the database. Replace `botkit.db` with
`BOTKIT_STORE_PATH` when it is set:

```bash
mise exec -- sqlite3 botkit.db \
  "INSERT INTO users (user_id, rank, registered_by) VALUES ('51999999999@s.whatsapp.net', 'user', 'operator'); INSERT INTO registered_groups (group_id, registered_by) VALUES ('120363000000000000@g.us', 'operator');"
```

Set `active` to `0` on a row to deactivate it. The tables are owned by
[`internal/auth`](../internal/auth).

## Commands

`!help` lists the commands. `!help latex` describes one.

Send the whole expression after `!latex`:

```text
!latex \frac{a}{b}
```

The bot replies with the PNG and reacts ✅. It reacts ❌ when the command fails.

## Greetings

An authorized user who sends `hi`, `hello`, `hey`, `hola`, `buenas`,
`buenos días`, `buenas tardes`, `buenas noches`, `!start`, or `!menu` gets a 👋
reaction and a welcome. The first greeting from a user gets the full welcome.
Later ones get a short welcome back. The first visit is stored in the
`latex_user_state` table, so a restart keeps it. Greetings from users who may
not run `help` get no reply.

## Equations

The bot renders one math expression with Typst and the Mitex 0.2.7 package,
which is embedded in the binary. It handles fractions, roots, sums, integrals,
matrices, `align`, `\text{...}`, and `\textbf{...}`. The bot also defines the
`physics` macros `\abs`, `\norm`, `\dv`, and `\pdv`.

Mitex does not know `\usepackage`, `\begin{document}`, `mhchem`'s `\ce`, or the
`physics` macro `\qty`. The bot replies with the first line of the renderer's
message and no image. It never downloads packages.

## Limits

[Configuration](configuration.md#latex-bot) sets the limits. A failed request
gets one of these replies:

| Cause                                    | Reply                                        |
| ---------------------------------------- | -------------------------------------------- |
| No expression after `!latex`             | A request for LaTeX, with an example.        |
| More than `LATEX_MAX_LENGTH` characters  | The limit and a request for a shorter input. |
| Time or memory limit, or a runaway macro | A notice to try a shorter or simpler input.  |
| An image over the pixel or byte limit    | A notice that the equation is too large.     |
| Any other Typst or Mitex error           | The first line of the renderer's message.    |

Each render runs in a private temporary directory that the bot removes when the
render ends.
