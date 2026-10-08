# Flow format

A flow is a JSON file of nodes. Each node has a message and the transitions that
lead out of it. [The flow bot](flow-bot.md) runs it.

Start from the built-in flow and edit a copy. `FLOW_FILE` applies to `run` and
to the REPL:

```bash
cp internal/flow/fsm/engaging.json my-flow.json
FLOW_FILE=my-flow.json mise exec -- bin/botkit-flow --repl
```

A smaller flow. The `help` global transition leads to the hand-off node:

```json
{
  "start_node": "WELCOME",
  "global_transitions": [
    {
      "condition": { "type": "keyword", "value": ["help"] },
      "target": "NEEDS_ASSISTANCE"
    }
  ],
  "nodes": {
    "WELCOME": {
      "message": { "content": "Hi {{name}}! Write *1* for prices." },
      "transitions": [
        {
          "condition": { "type": "exact", "value": ["1"] },
          "target": "PRICES",
          "react": "👍"
        }
      ]
    },
    "PRICES": {
      "message": { "content": "Everything costs 10." }
    },
    "NEEDS_ASSISTANCE": {
      "message": { "content": "A person will write to you." },
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

Every flow must contain a node named `NEEDS_ASSISTANCE`.

The bot reads the file once at startup and stops with an error when it cannot
read or validate it. It does not fall back to the built-in flow. A JSON syntax
error or an unknown field, such as the removed `message.type`, is reported
alone. The loader reports every other mistake in one pass, each with where it
sits. These include an invalid start node, a missing help node, a transition to
a missing node, an include of a missing group, an unknown condition type, an
invalid regular expression, a blank or missing condition value, an unknown media
kind, an action name the bot does not know, and a `react` that is not exactly
one emoji.

## Nodes

| Field                       | Meaning                                                  |
| --------------------------- | -------------------------------------------------------- |
| `message.content`           | The text the bot sends on entering the node.             |
| `title`                     | A label. `{{course_name}}` and `{{last_choice}}` use it. |
| `transitions`               | Transitions out of the node.                             |
| `include_transitions`       | Name of a transition group to try before `transitions`.  |
| `action`                    | Action that runs when the user enters the node.          |
| `react`                     | Emoji for a turn that enters the node.                   |
| `ignore_global_transitions` | Skip global transitions, except those to the help node.  |
| `fallback_message`          | Reply when no transition matches.                        |

A transition has a `condition`, a `target` node, an optional `action`, and an
optional `react`. `react` is exactly one emoji, such as `✅`. The bot reacts to
the user's message with it once the turn is stored and the replies are sent.
When a transition and the node it enters both have a `react`, the transition's
wins. A fallback and a turn whose action failed never react.

## Routing

A new or restarted conversation first sends the start node's message, then
routes the user's message from that node. If that message matches nothing, the
bot adds no fallback notice.

Otherwise the bot takes the first transition that matches, in this order:

1. The global transitions. A node with `ignore_global_transitions` considers
   only those whose target is `NEEDS_ASSISTANCE`, so help requests still work.
2. The node's included group, then its own transitions. For a message with
   media, transitions with a `media` or `media_type` condition go first, so a
   caption does not take a photo's place.

A transition's action runs before the action of the node it enters, unless both
name the same action. Whatever the user sends, the bot first lowercases the text
and trims it.

## Conditions

| Type         | Fields  | Matches                                                                                                                                                        |
| ------------ | ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `exact`      | `value` | The whole text equals a value, ignoring case and surrounding spaces.                                                                                           |
| `keyword`    | `value` | A value appears as whole words, ignoring case and accents. A one-word value also matches a word within two edits of it, when both have more than four letters. |
| `regex`      | `regex` | The text matches Go's regular-expression syntax, ignoring case.                                                                                                |
| `any_text`   | none    | Non-empty text without media.                                                                                                                                  |
| `media`      | none    | Any media message.                                                                                                                                             |
| `media_type` | `value` | Media of a listed kind: `image`, `video`, `audio`, `document`, or `sticker`.                                                                                   |

## Fallbacks

When no transition matches, the bot stays in the node and replies. The reply is
the `fallback_message`, or else a notice followed by the node's message. A node
with transitions but neither a `fallback_message` nor a message moves the user
to the start node and sends the start node's message. A node with no transitions
and no included group stays silent.

Media gets two more cases:

- A node that waits for media (it has a `media_type` transition) names the kinds
  it accepts when the message has another kind.
- A node that takes no media asks for text when the message is audio, a sticker,
  a video, or a document.

An action that fails keeps the user in the node and asks for a retry. The bot
asks for a person only when the user explicitly asks or an action sets the
hand-off flag ([hand-off](flow-bot.md#hand-off-to-a-person)).

## Actions

An action changes what the bot stores about the user. A name outside this table
fails the load. To react without storing anything, give the transition a `react`
and no action.

| Action                          | Effect                                                                 |
| ------------------------------- | ---------------------------------------------------------------------- |
| `save_user_name`                | Stores the user's name, without an introduction such as `me llamo`.    |
| `clear_user_name`               | Removes the stored name.                                               |
| `set_selected_course`           | Stores the node's ID for `{{course_name}}`.                            |
| `remember_choice`               | Stores the node's ID as `{{last_choice}}`.                             |
| `opt_in_follow_up`              | Records permission for [reminders](flow-bot.md#reminders).             |
| `opt_out_follow_up`             | Removes permission for reminders.                                      |
| `update_lead_interest_beginner` | Stores `beginner` as the interest.                                     |
| `update_lead_interest_advanced` | Stores `advanced` as the interest.                                     |
| `update_lead_consulted_price`   | Records that the user asked for prices.                                |
| `save_payment_voucher`          | Saves an image. A message of another kind sets `requires_human_agent`. |
| `escalate_to_human_agent`       | Sets `requires_human_agent`.                                           |

`save_user_name` keeps the old name when the text holds no name. It asks again
when the text does not look like a name.

## Templates

| Template           | Value                                                                                                                                 |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------- |
| `{{name}}`         | The user's first name, or `amigx` when none is known.                                                                                 |
| `{{greeting}}`     | A welcome for a user with at most two stored messages, else a welcome back.                                                           |
| `{{course_name}}`  | The title of the node that `set_selected_course` stored, or `el curso seleccionado` when that node has no title.                      |
| `{{persona}}`      | `Luma`, the built-in guide's name.                                                                                                    |
| `{{last_choice}}`  | The title of the node `remember_choice` stored, the node's ID when it has no title, or `ninguna opción todavía` when none was stored. |
| `{{history_hint}}` | The user's previous text message, cut to 80 characters and ended with `…` when cut. Empty when there is none.                         |

The bot sends any other template unchanged, and `{{course_name}}` too until
`set_selected_course` has run.
