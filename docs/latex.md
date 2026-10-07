# The LaTeX bot

The LaTeX bot registers the `!latex` command and answers in direct chats and
registered groups. A user needs the `user` or `owner` rank. The `user` rank can
run `help` and `latex`; the `owner` rank can run every command.

## Register access

Set an owner JID in `BOTKIT_OWNER_JIDS` to seed it on every start:

```dotenv
BOTKIT_OWNER_JIDS=51999999999@s.whatsapp.net
```

The seed creates a missing owner and leaves an existing rank or deactivated user
unchanged. Direct chats need a registered user but no group registration. After
the first start, register a group and its members with the `sqlite3` CLI that
`mise install` provides. Use the same path as `BOTKIT_STORE_PATH` by replacing
`botkit.db` below when it is set; `botkit.db` is the default:

```bash
mise exec -- sqlite3 botkit.db \
  "INSERT INTO users (user_id, rank, registered_by) VALUES ('51999999999@s.whatsapp.net', 'user', 'operator'); INSERT INTO registered_groups (group_id, registered_by) VALUES ('120363000000000000@g.us', 'operator');"
```

The authorization rules are implemented by [`internal/auth`](../internal/auth)
and the command routing by [`internal/command`](../internal/command).

## Render an equation

Send the complete expression after `!latex`:

```text
!latex \frac{a}{b}
```

The bot renders one math expression as a PNG. It supports common amsmath-style
input, including fractions, roots, sums, integrals, matrices, `align`,
`\text{...}`, and `\textbf{...}`. The `physics` macros `\abs`, `\norm`, `\dv`,
and `\pdv` are defined by the bot.

It does not process document structure or packages such as `\documentclass`,
`\usepackage`, and `\begin{document}`. It does not define `mhchem`'s `\ce` or
the `physics` macro `\qty`. An unsupported command returns the renderer's
message and no image.

## Limits and failures

The input, render, and PNG limits are configured in
[configuration.md](configuration.md). An input over `LATEX_MAX_LENGTH` is
refused. A render over its time or resource limit gets a short retry notice. An
image over the pixel or byte limit gets a size notice. Other Typst or Mitex
errors return the first line of the renderer's message.

The bot runs Typst with the Mitex 0.2.7 package embedded in the binary. It also
needs `prlimit` from util-linux to enforce memory and file-size limits. It does
not download packages at runtime.
