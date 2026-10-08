package flow_test

import (
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
)

const (
	welcomeNode     = "WELCOME"
	onboardingHello = "hola"
)

func (r *rig) greetingOf(node, name, greeting string) string {
	return flow.Render(r.flow.Nodes[node].Message.Content, map[string]string{
		templateName: name, "greeting": greeting, "persona": "Luma",
	})
}

func (r *rig) remember(t *testing.T, count int) {
	t.Helper()

	err := r.store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = welcomeNode
		state.UserName = testName

		messages := make([]flow.StoredMessage, count)
		for i := range messages {
			messages[i] = flow.StoredMessage{
				Timestamp: time.Now(), Direction: flow.Inbound, Content: onboardingHello, NodeID: welcomeNode,
			}
		}

		return messages, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

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

			err := env.say(t, "zzzz")
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireSent(t, env.sent(), env.greetingOf("WELCOME", "Ana", test.want))
		})
	}
}

func TestGreetingSurvivesACountThatFails(t *testing.T) {
	t.Parallel()

	env := start(t)
	env.remember(t, 0)

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

	requireSent(t, env.sent(), env.greetingOf(welcomeNode, testName, welcomeBack))
}

func TestNewUserWithoutUsableProfileNameIsCalledAmigx(t *testing.T) {
	t.Parallel()

	for _, profile := range []string{"", "xyz", "12"} {
		t.Run("profile name "+profile, func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.client.Deliver(bot.Message{Sender: userAna, Text: onboardingHello, PushName: profile})

			err := env.result(t)
			if err != nil {
				t.Fatalf("Handle() error = %v", err)
			}

			requireSent(t, env.sent(), env.greetingOf(welcomeNode, "amigx", welcome))
			requireEqual(t, "UserName", env.state(t).UserName, profile)
		})
	}
}

func TestMessageIsNotAnsweredWhenItsUserCannotBeStored(t *testing.T) {
	t.Parallel()

	for _, table := range []string{"user_state", "conversation_history"} {
		t.Run("without "+table, func(t *testing.T) {
			t.Parallel()

			env := start(t)
			env.seed(t, welcomeNode)

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
