# The flow bot

The flow bot answers direct messages and ignores groups. It has no rank check.
It stores each user's current node, collected values, and conversation history
in the SQLite database. A conversation older than 24 hours starts again at
`start_node`.

With no `FLOW_FILE`, the bot uses the built-in example. The example is a Spanish
bookshop flow that includes menus, course selection, payment vouchers, and a
human hand-off. Copy
[`internal/flow/fsm/example.json`](../internal/flow/fsm/example.json) as a
starting point for a custom flow.

## Flow file

A flow is a JSON object with these top-level fields:

| Field                | Required | Meaning                                   |
| -------------------- | -------- | ----------------------------------------- |
| `start_node`         | yes      | Node for a new or restarted conversation. |
| `nodes`              | yes      | Map of node IDs to node definitions.      |
| `global_transitions` | no       | Transitions considered from every node.   |
| `transition_groups`  | no       | Named transition lists included by nodes. |

Every flow must contain a node named `NEEDS_ASSISTANCE`. The loader rejects
unknown JSON fields, missing targets, an invalid start node, missing help node,
unknown conditions, invalid regular expressions, empty condition values, and
unknown media kinds. It reports all validation errors it finds.

## Nodes and transitions

A node has a `message` with `type` and `content`, and may have `title`,
`transitions`, `include_transitions`, `action`, `ignore_global_transitions`, and
`fallback_message`:

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

The bot tries included transitions, then the node's own transitions, then global
transitions. It takes the first matching transition. A transition action runs
before the action on the node entered. If a node sets
`ignore_global_transitions`, it skips global transitions except transitions to
`NEEDS_ASSISTANCE`.

## Conditions

| Type         | Fields  | Matches                                                                                                                               |
| ------------ | ------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `exact`      | `value` | The whole text equals a value, ignoring case and surrounding spaces.                                                                  |
| `keyword`    | `value` | A value appears as a whole word, ignoring case and accents. A one-word value longer than four letters also tolerates up to two typos. |
| `regex`      | `regex` | The text matches Go's regular-expression syntax, ignoring case.                                                                       |
| `any_text`   | none    | Non-empty text without media.                                                                                                         |
| `media`      | none    | Any media message.                                                                                                                    |
| `media_type` | `value` | Media of a listed kind: `image`, `video`, `audio`, `document`, or `sticker`.                                                          |

## Fallbacks and hand-off

When no transition matches, the bot repeats the node's message after a short
notice. A `fallback_message` replaces that notice. A node with no transitions
stays silent. A node waiting for media explains which kind it accepts when the
message has the wrong kind.

Three fallbacks in a row move the user to `NEEDS_ASSISTANCE` and set
`requires_human_agent`. An action failure also tells the user a person will help
and marks the state. The bot does not notify a person outside the stored flag,
so an operator must monitor that state.

## Actions

The flow validates action names at startup. The supported actions are:

| Action                          | Effect                                                             |
| ------------------------------- | ------------------------------------------------------------------ |
| `create_new_lead`               | Marks the lead's starting point without changing state.            |
| `save_user_name`                | Stores the user's name, accepting common Spanish introductions.    |
| `clear_user_name`               | Removes the stored name.                                           |
| `set_selected_course`           | Stores the node ID for `{{course_name}}`.                          |
| `update_lead_interest_beginner` | Stores `beginner` as the interest.                                 |
| `update_lead_interest_advanced` | Stores `advanced` as the interest.                                 |
| `update_lead_consulted_price`   | Records that the user asked for prices.                            |
| `save_payment_voucher`          | Downloads and saves an image. A non-image marks the user for help. |
| `escalate_to_human_agent`       | Sets `requires_human_agent`.                                       |

Voucher files are stored under `FLOW_VOUCHER_DIR` with private file mode. The
directory is created when needed.

## Templates

The bot replaces these templates when it sends a node message:

| Template          | Value                                                      |
| ----------------- | ---------------------------------------------------------- |
| `{{name}}`        | The user's first name, or `amigx` when none is known.      |
| `{{greeting}}`    | A new-user or returning-user greeting.                     |
| `{{course_name}}` | The title of the node remembered by `set_selected_course`. |

Unknown templates are sent unchanged. The loader, router, and flow actions are
implemented in [`internal/flow/fsm`](../internal/flow/fsm) and
[`internal/flow`](../internal/flow).
