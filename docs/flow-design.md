# Flow bot redesign

## What it does today

The flow bot is a direct-message-only JSON finite-state machine. `cmd/flow`
loads `internal/flow/fsm/engaging.json` (or `FLOW_FILE`), then wires it to
`internal/flow.App`. Each message is one `Store.Turn`:

- `internal/flow/fsm` matches exact values, keywords, regular expressions,
  arbitrary text, and media. It enters the first matching node and runs the
  transition/node action.
- `internal/flow/store.go` persists one `user_state` row and appends
  `conversation_history` rows in the same SQLite transaction. It serializes
  turns per user, but the app only reads the message count for its greeting.
- `internal/flow/actions.go` stores a name, course interest, selected course,
  price interest, payment-voucher path, and human-escalation flag. Actions can
  save a payment image, but the flow sends no decorative outbound media: a
  club's schedule and price are already clearer in its short text reply.
- `internal/flow/app.go` renders node text, re-asks the current step on a
  fallback, and sends replies after saving. It has no dead-end escalation
  counter; follow-up is available only through explicit opt-in and a
  rate-limited method called by the production scheduler.

This is a useful flow runner, but it is not yet a good conversational companion.
The previous built-in flow starts with a two-choice menu and repeatedly asks
people to type labels such as `inscribirme`, even when their intent is obvious
from ordinary language.

## Why people stop answering

The failure is visible in the interaction, not just in the implementation:

1. A menu asks the user to translate their intent into the bot's vocabulary. A
   person saying “I want something for mystery novels” gets a menu or an
   unrelated fallback instead of a helpful next question.
2. Fallbacks explain that the answer was not understood, then show the same wall
   of options. After the third attempt the bot hands the conversation off
   without repairing the turn. That feels like a dead end, especially after a
   harmless typo.
3. The bot remembers a name and selected course in SQLite, but does not use a
   durable last choice or a compact history cue to make a later answer feel
   personal. A returning person starts at the same menu rather than resuming
   naturally.
4. Successful actions look like ordinary text. There is no small “done” signal
   and no clear rule for when media adds information. Conversely, unsolicited
   follow-up is not modelled at all, so adding it safely would be guesswork.
5. The current flow has a pleasant Spanish tone in places, but no named persona
   or consistent voice. Users cannot tell whether they are speaking to a helpful
   guide or filling in a form.

The reference [Jibaru/wspbot](https://github.com/Jibaru/wspbot) makes the
opposite tradeoffs: it names a personality, keeps memory/history, uses short
confirmations, chooses media and reactions when they add information, and only
chimes in for configured groups. The bots-v2 notes in the repository's history
record the same useful patterns: clear personality, opt-in behavior, memory,
useful media, reactions, and short confirmations. This redesign keeps those
patterns within botkit's existing `bot.Transport` boundary so they can be tested
offline.

## Concrete changes

| Observed weakness                                    | Change                                                                                                                                                                                                                                                                                                       | User-visible result                                                                                                   |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------- |
| The built-in experience is a menu tree.              | Replace the old built-in flow with **Luma**, a named reading-club concierge. Its guided steps accept natural-language intent and aliases before offering a small choice.                                                                                                                                     | “I like detective novels” advances directly to the mystery-club explanation; the user is not forced to find a number. |
| A typo can lead to a three-strike handoff.           | In the engaging experience, classify free text with the existing keyword/typo matcher and keep recovery in the current step. Every fallback repeats the compact options and suggests examples; it never silently ends the conversation.                                                                      | `misteryo`, “something on Saturdays”, or “not sure” gets a useful re-ask.                                             |
| Memory is only name/course interpolation.            | Add SQLite fields for `last_choice`, unbounded conversation history, and follow-up consent/time. Render `{{last_choice}}` and a bounded `{{history_hint}}`; use them in return, confirmation, and recall replies.                                                                                            | Luma can say which club the person last explored and resume from it without dumping an unbounded transcript.          |
| Success is not acknowledged, and media is underused. | React only after a stored enrolling or lead-changing action. Keep receipt-image handling for payment verification, but do not add a decorative club card when the same schedule and price are already in text. Informational, choice-viewing, consent, fallback, and failed routes send no success reaction. | Enrollment gets one `✅`; exploring a club, a typo, or a failed download does not.                                    |
| There is no safe proactive behavior.                 | Add explicit `recordatorios` opt-in and an app follow-up method that selects only opted-in users, enforces a SQLite time window, and records the send before returning.                                                                                                                                      | Luma never starts a chat without consent and cannot send repeated reminders inside the rate window.                   |
| The voice is generic.                                | Put Luma's name, warm concise Spanish voice, and response-length rule in the built-in flow and persona constants.                                                                                                                                                                                            | Replies sound like one useful guide rather than a form renderer.                                                      |

The custom `FLOW_FILE` contract remains available for operators who need a
different workflow. The new behavior is exercised through the real SQLite store
and `internal/whatsapp/fake`; the offline REPL is also used as a manual
transcript check with `go run ./cmd/flow --repl`.
