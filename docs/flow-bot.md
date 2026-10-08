# The flow bot

The flow bot walks each user through a conversation that you write as a JSON
file ([flow format](flow-format.md)). It answers direct messages and ignores
groups. It checks no rank, so anyone who may message the account gets a
conversation, subject to `BOTKIT_ALLOW_ONLY` and the rate limit
([configuration](configuration.md)).

Without `FLOW_FILE` it runs the flow built into the binary: a reading-club guide
named Luma that replies in Spanish. Users describe what they want in ordinary
words, and a misspelled keyword still matches. Try it offline:

```bash
mise exec -- bin/botkit-flow --repl
```

```text
you> hola
bot> ¡Bienvenidx! Es un placer ayudarte a empezar. Soy *Luma*, tu guía de clubes de lectura. Cuéntame qué te apetece — misterio, poesía, algo para empezar — o pregunta por precios y horarios.
you> misteryo
bot> Buena pista, Repl. *Club Misterio* se reúne los martes a las 7 p. m. (S/ 40 al mes). ¿Te apunto?
you> sí
bot> Listo, Repl ✅ Dejé tu interés en *Club Misterio*. Si quieres, envíame una foto del comprobante y la guardo para el equipo.
bot> reaction ✅
```

## What the bot remembers

For each user the bot stores the current node, the name, the last choice,
reminder consent, and the whole conversation history in SQLite. The built-in
flow uses them: `qué elegí` makes Luma repeat the last choice and the user's
previous message.

A conversation that rests for more than 24 hours starts again at `start_node`.
The stored memory stays. The next message gets the start node's message, then is
routed from the start node ([routing](flow-format.md#routing)). The built-in
start message opens with `{{greeting}}`, which a flow author may leave out
([templates](flow-format.md#templates)). A reminder the bot sent counts as
activity, so a reply to it resumes where the user paused.

The bot reacts ✅ after the actions that
[the flow format](flow-format.md#actions) marks, never after a fallback or a
failed action.

## Reminders

A user who opts in gets at most one reminder every 24 hours. In the built-in
flow, the user says `recordatorios` and then `sí`. A flow opts a user in with
the `opt_in_follow_up` action and out with `opt_out_follow_up`. A message opts a
user in only through a transition or node that runs `opt_in_follow_up`.

While `run` is active, the bot checks once an hour for opted-in users whose last
reminder is at least 24 hours old. It sends each a fixed Spanish text that names
the title of their last choice:
`Hola, soy Luma 👋 ¿Seguimos con Club Misterio? Responde cuando te venga bien.`
When the user has no last choice, the text says `algo nuevo` in its place. When
the last choice is a node with no title, it names the node's ID. The text does
not come from the flow file. The REPL never sends reminders.

A send that fails is retried at a later check. After a send that succeeds, the
user gets no other reminder for 24 hours.
[Architecture](architecture.md#reminders) explains how the bot claims a reminder
before it sends.

## Hand-off to a person

The bot sends no message to a person. The `escalate_to_human_agent` action, or
`save_payment_voucher` on a message that is not an image, sets
`requires_human_agent` in the `user_state` table. The bot never clears it. List
the users who need help:

```bash
mise exec -- sqlite3 botkit.db \
  "SELECT user_id, current_node FROM user_state WHERE requires_human_agent;"
```

After you have helped a user, clear the flag:

```bash
mise exec -- sqlite3 botkit.db \
  "UPDATE user_state SET requires_human_agent = 0 WHERE user_id = '51999999999@s.whatsapp.net';"
```

Replace `botkit.db` with `BOTKIT_STORE_PATH` when it is set.

## Vouchers

`save_payment_voucher` saves an image a user sends, such as a payment receipt.
The file is named `{phone}_{profile name}_{unix seconds}_{random}.jpeg`, with
the profile name cut to ASCII letters, digits, hyphens, and underscores (`user`
when nothing is left). It lives in `FLOW_VOUCHER_DIR` with mode 0600. The bot
creates the directory when the first voucher arrives. The `voucher_path` column
holds the user's latest voucher.

## Stored data

The bot keeps two tables in the shared SQLite file: `user_state`, one row per
user, and `conversation_history`, one row per message in either direction.
[Architecture](architecture.md#flow-state) lists the columns and which code
writes them.
