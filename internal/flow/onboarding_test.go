//nolint:goconst // Node names and texts repeat across cases; literals keep each case readable against the example flow.
package flow_test

import (
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
)

// greetingOf is what the flow says on entering node to a user called name.
func (r *rig) greetingOf(node, name, greeting string) string {
	return flow.Render(r.flow.Nodes[node].Message.Content, map[string]string{"name": name, "greeting": greeting})
}

// remember stores count messages of userAna, as if they had been talking a while.
func (r *rig) remember(t *testing.T, count int) {
	t.Helper()

	err := r.store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = "GREETING_INTRO"
		state.UserName = "Ana"

		messages := make([]flow.StoredMessage, count)
		for i := range messages {
			messages[i] = flow.StoredMessage{Timestamp: time.Now(), Direction: flow.Inbound, Content: "hola", NodeID: "GREETING_INTRO"}
		}

		return messages, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// idleFor makes userAna's last message that old.
func (r *rig) idleFor(t *testing.T, age time.Duration) {
	t.Helper()

	_, err := r.db.ExecContext(t.Context(), `UPDATE user_state SET last_updated = ? WHERE user_id = ?`,
		time.Now().Add(-age).UTC(), userAna)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGreetingFollowsTheStoredMessageCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages int
		want     string
	}{
		{"no messages yet", 0, welcome},
		{"two messages", 2, welcome},
		{"three messages", 3, welcomeBack},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.remember(t, test.messages)
			env.idleFor(t, 25*time.Hour)

			// After a day the start node greets again, and nothing else does.
			err := env.say(t, "zzzz")
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireSent(t, env.sent(), env.greetingOf("GREETING_INTRO", "Ana", test.want))
		})
	}
}

// A count that cannot be read must not keep a user from being answered, and
// the user is welcomed back as one who has talked before.
func TestGreetingSurvivesACountThatFails(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.remember(t, 0)

	// The count query fails, and the turn still stores its messages through the
	// view: abs of the smallest integer overflows.
	_, err := env.db.ExecContext(t.Context(), `
		ALTER TABLE conversation_history RENAME TO stored_history;
		CREATE VIEW conversation_history AS SELECT id, user_id, timestamp, direction, message_content, node_id FROM stored_history WHERE abs(-9223372036854775808) > 0;
		CREATE TRIGGER store_through_the_view INSTEAD OF INSERT ON conversation_history BEGIN
			INSERT INTO stored_history (user_id, timestamp, direction, message_content, node_id)
			VALUES (NEW.user_id, NEW.timestamp, NEW.direction, NEW.message_content, NEW.node_id);
		END;`)
	if err != nil {
		t.Fatal(err)
	}

	env.idleFor(t, 25*time.Hour)

	err = env.say(t, "zzzz")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireSent(t, env.sent(), env.greetingOf("GREETING_INTRO", "Ana", welcomeBack))

	var logged int

	for _, record := range env.logged(t) {
		if record["msg"] == "cannot count messages for the greeting" && record["level"] == "ERROR" {
			logged++
		}
	}

	requireEqual(t, "logged count failures", logged, 1)
}

func TestNewUserWithoutUsableProfileNameIsCalledAmigx(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"", "xyz", "12"} {
		t.Run("profile name "+profile, func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.client.Deliver(bot.Message{Sender: userAna, Text: "hola", PushName: profile})

			err := env.result(t)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireSent(t, env.sent(), env.greetingOf("GREETING_INTRO", "amigx", welcome))
			requireEqual(t, "UserName", env.state(t).UserName, profile)
		})
	}
}

func TestStartNodeWithoutAMessageSaysNothing(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(tiny(t, `"START":{"message":{"type":"text","content":""}}`)))

	err := env.say(t, "hola")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireSent(t, env.sent())
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "START")
	requireHistory(t, env.history(t), exchange{flow.Inbound, "START", "hola"})
}

func TestConversationRestartsOnlyAfterADay(t *testing.T) {
	t.Parallel()

	returning := func(state *flow.State) {
		state.UserName = "Carla"
		state.CourseInterest = "beginner"
		state.SelectedCourseID = "CLUB_MISTERIO"
		state.RepromptCount = 2
	}

	tests := []struct {
		name string
		idle time.Duration
		text string
		// want is the replies, and node where the user ends up.
		want []string
		node string
	}{
		{"within a day the conversation goes on", 23 * time.Hour, "precio", []string{"CONSULTED_PRICE"}, "CONSULTED_PRICE"},
		{"after a day an answer to the menu is acted on", 25 * time.Hour, "1", []string{"GREETING_INTRO", "INTERESTED_IN_BEGINNER"}, "INTERESTED_IN_BEGINNER"},
		{"after a day anything else only gets the greeting", 25 * time.Hour, "zzzz", []string{"GREETING_INTRO"}, "GREETING_INTRO"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.seed(t, "INTERESTED_IN_BEGINNER", returning)
			env.idleFor(t, test.idle)

			err := env.say(t, test.text)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			want := make([]string, 0, len(test.want))
			for _, node := range test.want {
				want = append(want, env.greetingOf(node, "Carla", welcome))
			}

			requireSent(t, env.sent(), want...)

			state := env.state(t)
			requireEqual(t, "CurrentNode", state.CurrentNode, test.node)
			requireEqual(t, "RepromptCount", state.RepromptCount, 0)
			requireEqual(t, "UserName", state.UserName, "Carla")
			requireEqual(t, "SelectedCourseID", state.SelectedCourseID, "CLUB_MISTERIO")
			requireEqual(t, "CourseInterest", state.CourseInterest, "beginner")
		})
	}
}

// A user with a name and no node is one the bot has never answered, however
// old the row is.
func TestStateWithoutANodeIsOnboardedEvenIfOld(t *testing.T) {
	t.Parallel()

	env := start(t)

	err := env.store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.UserName = "Old"

		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	env.idleFor(t, 72*time.Hour)

	err = env.say(t, "hola")
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	requireSent(t, env.sent(), env.text("GREETING_INTRO", welcome))
	requireEqual(t, "CurrentNode", env.state(t).CurrentNode, "GREETING_INTRO")
	requireEqual(t, "UserName", env.state(t).UserName, "Ana")
}

// A message the bot cannot store is neither answered nor half kept.
func TestMessageIsNotAnsweredWhenItsUserCannotBeStored(t *testing.T) {
	t.Parallel()

	for _, table := range []string{"user_state", "conversation_history"} {
		t.Run("without "+table, func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.seed(t, "MAIN_MENU")

			_, err := env.db.ExecContext(t.Context(), `DROP TABLE `+table)
			if err != nil {
				t.Fatal(err)
			}

			err = env.say(t, "precios")
			if err == nil || !strings.Contains(err.Error(), table) {
				t.Fatalf("Handle() error = %v, want one naming %s", err, table)
			}

			requireSent(t, env.sent())
		})
	}
}
