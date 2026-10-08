# The flow bot

The flow bot answers direct messages from anyone and ignores groups. The
built-in experience is **Luma**, a warm, concise reading-club guide: users can
describe what they want in ordinary language, recover from typos, and ask what
Luma remembers. It checks no rank. It stores each user's current node, name,
last choice, follow-up consent, and conversation history in SQLite. A
conversation that rests for more than 24 hours starts again at `start_node`
without erasing that memory. A recent claimed reminder also keeps the current
flow live for that window, so a reply to the reminder resumes where it paused.

The default binary uses the engaging built-in flow in
`internal/flow/fsm/engaging.json`. `FLOW_FILE` still loads a custom flow. The
same actions, memory templates, fake transport, and recovery rules are available
to custom flows. A `✅` reaction is sent only after a meaningful successful
action, never for informational, help, consent, fallback, or failed routes.

## Write a flow

A flow is a JSON file of nodes. Each node has a message and the transitions that
lead out of it. Start from the built-in reading-club flow and edit a copy:

```bash
cp internal/flow/fsm/engaging.json my-flow.json
FLOW_FILE=my-flow.json mise exec -- bin/botkit-flow --repl
```

`FLOW_FILE` also applies to `run`. The bot reads the file once at startup and
exits with an error for a file it cannot read or validate. Without `FLOW_FILE`
it runs Luma's built-in experience.

A smaller flow:

```json
{
  "start_node": "WELCOME",
  "nodes": {
    "WELCOME": {
      "message": {
        "type": "text",
        "content": "Hi {{name}}! Write *1* for prices."
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
  }
}
```

## Flow file

| Field                | Required | Meaning                                   |
| -------------------- | -------- | ----------------------------------------- |
| `start_node`         | yes      | Node for a new or restarted conversation. |
| `nodes`              | yes      | Map of node IDs to node definitions.      |
| `global_transitions` | no       | Transitions considered from every node.   |
| `transition_groups`  | no       | Named transition lists included by nodes. |

Every flow must contain a node named `NEEDS_ASSISTANCE`. The loader rejects
unknown JSON fields, an invalid start node, a missing help node, transitions to
missing nodes, includes of missing groups, unknown conditions, invalid regular
expressions, blank or missing condition values, and unknown media kinds. It
reports every such error at once. Once the file is valid, the bot also rejects
action names it does not know, so a typo stops the bot at startup.

## Nodes

| Field                       | Meaning                                                 |
| --------------------------- | ------------------------------------------------------- |
| `message.content`           | The text the bot sends on entering the node.            |
| `message.type`              | Set it to `text`. The bot does not read it.             |
| `title`                     | A label. `{{course_name}}` uses it.                     |
| `transitions`               | Transitions out of the node.                            |
| `include_transitions`       | Name of a transition group to try before `transitions`. |
| `action`                    | Action that runs when the user enters the node.         |
| `ignore_global_transitions` | Skip global transitions, except those to the help node. |
| `fallback_message`          | Reply when no transition matches.                       |

A transition has a `condition`, a `target` node, and an optional `action`.

## Routing

The bot takes the first transition that matches, in this order:

1. The global transitions. A node with `ignore_global_transitions` considers
   only those whose target is `NEEDS_ASSISTANCE`. This keeps informational,
   help, and opt-out commands from being shadowed by a local fuzzy keyword.
2. The node's included group, then its own transitions. For a message with
   media, transitions with a `media` or `media_type` condition go first, so a
   caption does not take a photo's place.

A transition's action runs before the action of the node it enters, unless both
name the same action.

## Conditions

| Type         | Fields  | Matches                                                                                                                                                        |
| ------------ | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `exact`      | `value` | The whole text equals a value, ignoring case and surrounding spaces.                                                                                           |
| `keyword`    | `value` | A value appears as whole words, ignoring case and accents. A one-word value also matches a word within two edits of it, when both have more than four letters. |
| `regex`      | `regex` | The text matches Go's regular-expression syntax, ignoring case.                                                                                                |
| `any_text`   | none    | Non-empty text without media.                                                                                                                                  |
| `media`      | none    | Any media message.                                                                                                                                             |
| `media_type` | `value` | Media of a listed kind: `image`, `video`, `audio`, `document`, or `sticker`.                                                                                   |

## Fallbacks and hand-off

When no transition matches, the bot replies with a notice and the node's
message. A `fallback_message` replaces both. A node with no transitions and no
included group stays silent. A node that waits for media names the kinds it
accepts when the message has another kind. A node that takes no media asks for
text when the message is audio, a sticker, a video, or a document.

An unmatched message stays in the current step and asks again with compact
examples; it never turns a mistyped answer into a dead end. An action failure
also keeps the user in that step and asks for a retry. A person is requested
only when the user explicitly asks for help or a flow action sets the hand-off
flag.

The bot sends no message to a person. It stores the flag in `user_state`. List
the users who need help with:

```bash
mise exec -- sqlite3 botkit.db \
  "SELECT user_id, current_node FROM user_state WHERE requires_human_agent;"
```

## Actions

| Action                          | Effect                                                                 |
| ------------------------------- | ---------------------------------------------------------------------- |
| `create_new_lead`               | Nothing. It names a node in the flow file.                             |
| `save_user_name`                | Stores the user's name, without an introduction such as `me llamo`.    |
| `clear_user_name`               | Removes the stored name.                                               |
| `set_selected_course`           | Stores the node's ID for `{{course_name}}`.                            |
| `remember_choice`               | Stores the guide node the user explored as `{{last_choice}}`.          |
| `opt_in_follow_up`              | Records explicit permission for proactive reminders.                   |
| `opt_out_follow_up`             | Removes permission for proactive reminders.                            |
| `update_lead_interest_beginner` | Stores `beginner` as the interest.                                     |
| `update_lead_interest_advanced` | Stores `advanced` as the interest.                                     |
| `update_lead_consulted_price`   | Records that the user asked for prices.                                |
| `save_payment_voucher`          | Saves an image. A message of another kind sets `requires_human_agent`. |
| `escalate_to_human_agent`       | Sets `requires_human_agent`.                                           |

The built-in flow keeps club facts in its short text reply. It sends no
decorative outbound media: the only media path is an inbound receipt image,
because that image carries payment information the text cannot replace.

A voucher is saved as `{phone}_{profile name}_{unix seconds}_{random}.jpeg` in
`FLOW_VOUCHER_DIR`, with mode 0600. The bot creates the directory with the first
voucher.

## Templates

| Template           | Value                                                                       |
| ------------------ | --------------------------------------------------------------------------- |
| `{{name}}`         | The user's first name, or `amigx` when none is known.                       |
| `{{greeting}}`     | A welcome for a user with at most two stored messages, else a welcome back. |
| `{{course_name}}`  | The title of the node that `set_selected_course` stored.                    |
| `{{persona}}`      | `Luma`, the built-in guide's name.                                          |
| `{{last_choice}}`  | The title of the last remembered choice, or a neutral empty-state phrase.   |
| `{{history_hint}}` | A bounded recent inbound message, for a compact memory reply.               |

The bot sends any other template unchanged, and `{{course_name}}` too until
`set_selected_course` has run.

## Stored data

The `user_state` and `conversation_history` tables hold one row per user and one
row per message. The store writes a user's state and messages in one
transaction, then sends the replies. Turns for one user run one at a time. Run
one process on each database, because the lock lives in the process.
`App.FollowUp` claims only opted-in users whose last reminder is at least 24
hours old, sends each claim independently, releases a claim when its send fails,
and records each successful reminder in `conversation_history`. The claim is the
send attempt: a successful send remains rate-limited even if recording its
history row fails, so the scheduler never duplicates a delivered reminder. A
claim made before an inbound opt-out is still sent; the scheduler does not
re-check consent after claiming. Scheduler writes do not change `last_updated`;
the app uses the newer `last_follow_up` claim only to keep a live flow open for
the reminder's 24-hour window. An old reminder cannot prevent a stale restart.
It uses the remembered choice in the message. The production hourly scheduler
calls it; inbound messages never opt a person in implicitly.
