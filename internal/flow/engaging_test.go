//nolint:goconst,noinlineerr,wsl_v5 // Conversation scripts read as transcripts.
package flow_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/flow/fsm"
)

type selectiveFollowUpTransport struct {
	bot.Transport

	fail bot.JID
}

type cancelAfterSendTransport struct {
	bot.Transport

	cancel context.CancelFunc
}

var errFollowUpUnavailable = errors.New("follow-up recipient unavailable")

func (t selectiveFollowUpTransport) SendText(ctx context.Context, recipient bot.JID, text string) error {
	if recipient == t.fail {
		return errFollowUpUnavailable
	}

	err := t.Transport.SendText(ctx, recipient, text)
	if err != nil {
		return fmt.Errorf("send text: %w", err)
	}

	return nil
}

func requireLastFollowUpNull(t *testing.T, env *rig, user bot.JID, wantNull bool) {
	t.Helper()

	var isNull bool
	err := env.db.QueryRowContext(t.Context(),
		`SELECT last_follow_up IS NULL FROM user_state WHERE user_id = ?`, user,
	).Scan(&isNull)
	if err != nil {
		t.Fatal(err)
	}
	if isNull != wantNull {
		t.Errorf("last_follow_up IS NULL = %v, want %v", isNull, wantNull)
	}
}

func (t cancelAfterSendTransport) SendText(ctx context.Context, recipient bot.JID, text string) error {
	err := t.Transport.SendText(ctx, recipient, text)
	t.cancel()

	if err != nil {
		return fmt.Errorf("send text: %w", err)
	}

	return nil
}

func engagingFlow(t *testing.T) *fsm.Flow {
	t.Helper()

	definition, err := fsm.EngagingExample()
	if err != nil {
		t.Fatal(err)
	}

	return definition
}

func sayContains(t *testing.T, env *rig, text, want string) {
	t.Helper()

	if err := env.say(t, text); err != nil {
		t.Fatal(err)
	}

	replies := env.sent()
	if len(replies) != 1 || !strings.Contains(replies[0], want) {
		t.Fatalf("reply to %q = %q, want %q", text, replies, want)
	}
}

func TestEngagingConversationUsesVoiceMemoryAndRecovery(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))

	sayContains(t, env, "hola", "Soy *Luma*")
	if got := env.client.Reactions(); len(got) != 0 {
		t.Fatalf("welcome reactions = %+v, want none before success", got)
	}

	sayContains(t, env, "misteryo", "Club Misterio")
	if got := env.client.Reactions(); len(got) != 0 {
		t.Fatalf("club-view reactions = %+v, want none before enrollment", got)
	}

	sayContains(t, env, "tal vez", "Responde *sí*")
	if got := env.client.Reactions(); len(got) != 0 {
		t.Fatalf("recovery reactions = %+v, want no new reaction", got)
	}

	sayContains(t, env, "sí", "Club Misterio")
	if got := env.client.Reactions(); len(got) != 1 || got[0].Emoji != "✅" {
		t.Fatalf("enrollment reactions = %+v, want one success reaction", got)
	}

	sayContains(t, env, "qué elegí", "Club Misterio")

	state := env.state(t)
	if state.LastChoice != "MYSTERY" {
		t.Errorf("LastChoice = %q, want MYSTERY", state.LastChoice)
	}
}

func TestInformationalAndConsentRoutesDoNotReact(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	for _, text := range []string{"hola", "quien eres", "recordatorios", "sí", "ayuda"} {
		if err := env.say(t, text); err != nil {
			t.Fatal(err)
		}
		env.sent()
	}

	if got := env.client.Reactions(); len(got) != 0 {
		t.Fatalf("informational/consent reactions = %+v, want none", got)
	}
}

func TestOptOutWinsFromMemoryAndStopsFollowUps(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	for _, text := range []string{"hola", "misterio", "recordatorios", "sí", "qué elegí", "no mas recordatorios"} {
		if err := env.say(t, text); err != nil {
			t.Fatal(err)
		}
		env.sent()
	}

	state := env.state(t)
	if state.FollowUpOptIn {
		t.Fatal("FollowUpOptIn = true after opt-out, want false")
	}
	if state.CurrentNode != "FOLLOW_UP_OFF" {
		t.Fatalf("CurrentNode = %q after opt-out, want FOLLOW_UP_OFF", state.CurrentNode)
	}
	if sent, err := env.app.FollowUp(t.Context(), env.client, time.Now()); err != nil || sent != 0 {
		t.Fatalf("FollowUp() after opt-out = %d, %v; want zero", sent, err)
	}
}

func TestEngagingFailedMediaActionCanBeRetried(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	for _, text := range []string{"hola", "misterio", "sí"} {
		if err := env.say(t, text); err != nil {
			t.Fatal(err)
		}
		env.sent()
	}

	beforeReactions := len(env.client.Reactions())
	err := env.sendMedia(t, bot.MediaImage, "not downloadable")
	if err == nil || !strings.Contains(err.Error(), "download") {
		t.Fatalf("failed media error = %v, want download failure", err)
	}

	replies := env.sent()
	if len(replies) != 1 || !strings.Contains(replies[0], "Inténtalo una vez más") {
		t.Fatalf("failed media replies = %q, want a retry", replies)
	}
	if got := len(env.client.Reactions()); got != beforeReactions {
		t.Errorf("failed media reactions = %d, want %d", got, beforeReactions)
	}
	if state := env.state(t); state.CurrentNode != "BOOKED" {
		t.Errorf("failed media node = %q, want BOOKED for retry", state.CurrentNode)
	}
}

func TestOptInFollowUpIsRateLimited(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	for _, text := range []string{"hola", "misterio", "recordatorios", "sí"} {
		if err := env.say(t, text); err != nil {
			t.Fatal(err)
		}
		env.sent()
	}

	sent, err := env.app.FollowUp(t.Context(), env.client, time.Now())
	if err != nil || sent != 1 {
		t.Fatalf("first FollowUp() = %d, %v; want one", sent, err)
	}
	allSent := env.client.Sent()
	if got := allSent[len(allSent)-1].Text; !strings.Contains(got, "Club Misterio") {
		t.Errorf("follow-up = %q, want the remembered choice", got)
	}

	sent, err = env.app.FollowUp(t.Context(), env.client, time.Now())
	if err != nil || sent != 0 {
		t.Fatalf("second FollowUp() = %d, %v; want rate-limited zero", sent, err)
	}
}

func TestNeverFollowedUpUserStoresANullClaim(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	env.seed(t, "WELCOME")

	requireLastFollowUpNull(t, env, userAna, true)
}

func TestFollowUpReleasesOnlyFailedClaimsAndStoresSuccessfulHistory(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	for _, user := range []bot.JID{userAna, userLuis} {
		err := env.store.Turn(t.Context(), user, func(state *flow.State) ([]flow.StoredMessage, error) {
			state.CurrentNode = "WELCOME"
			state.LastChoice = "MYSTERY"
			state.FollowUpOptIn = true

			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	sent, err := env.app.FollowUp(t.Context(), selectiveFollowUpTransport{Transport: env.client, fail: userAna}, time.Now())
	if sent != 1 || err == nil {
		t.Fatalf("mixed FollowUp() = %d, %v; want one send and one error", sent, err)
	}

	requireLastFollowUpNull(t, env, userAna, true)
	requireLastFollowUpNull(t, env, userLuis, false)

	history := env.historyFor(t, userLuis)
	if len(history) != 1 || history[0].Direction != flow.Outbound {
		t.Fatalf("successful follow-up history = %+v, want one outbound message", history)
	}

	sent, err = env.app.FollowUp(t.Context(), env.client, time.Now().Add(time.Minute))
	if err != nil || sent != 1 {
		t.Fatalf("retry FollowUp() = %d, %v; want the released user", sent, err)
	}
}

func TestFollowUpKeepsDeliveredClaimWhenHistoryRecordFails(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	err := env.store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = "WELCOME"
		state.LastChoice = "MYSTERY"
		state.FollowUpOptIn = true

		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.ExecContext(t.Context(), "DROP TABLE conversation_history"); err != nil {
		t.Fatal(err)
	}

	sent, err := env.app.FollowUp(t.Context(), env.client, time.Now())
	if sent != 1 || err == nil {
		t.Fatalf("FollowUp() = %d, %v; want one delivered send and an error", sent, err)
	}
	requireLastFollowUpNull(t, env, userAna, false)
}

func TestFollowUpKeepsDeliveredClaimWithCanceledCallerContext(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	err := env.store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = "WELCOME"
		state.LastChoice = "MYSTERY"
		state.FollowUpOptIn = true

		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.ExecContext(t.Context(), "DROP TABLE conversation_history"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	transport := cancelAfterSendTransport{Transport: env.client, cancel: cancel}
	sent, err := env.app.FollowUp(ctx, transport, time.Now())
	if sent != 1 || err == nil {
		t.Fatalf("FollowUp() = %d, %v; want one delivered send and an error", sent, err)
	}
	requireLastFollowUpNull(t, env, userAna, false)
}

func TestReplyToRecentFollowUpResumesTheConversation(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	env.seed(t, "MYSTERY", func(state *flow.State) {
		state.LastChoice = "MYSTERY"
		state.FollowUpOptIn = true
	})
	if _, err := env.db.ExecContext(t.Context(), `UPDATE user_state SET last_updated = ? WHERE user_id = ?`, time.Now().UTC().Add(-25*time.Hour), userAna); err != nil {
		t.Fatal(err)
	}
	before := env.state(t).LastUpdated

	sent, err := env.app.FollowUp(t.Context(), env.client, time.Now())
	if err != nil || sent != 1 {
		t.Fatalf("FollowUp() = %d, %v; want one reminder", sent, err)
	}
	if got := env.state(t).LastUpdated; !got.Equal(before) {
		t.Fatalf("LastUpdated after reminder = %v, want unchanged %v", got, before)
	}
	env.sent()

	if err := env.say(t, "sí"); err != nil {
		t.Fatal(err)
	}
	if replies := env.sent(); len(replies) != 1 || !strings.Contains(replies[0], "Listo") {
		t.Fatalf("reply after reminder = %q, want the live club enrollment", replies)
	}
}

func TestUnpromptedReturnAfterReminderStalenessRestarts(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	env.seed(t, "MYSTERY", func(state *flow.State) {
		state.LastChoice = "MYSTERY"
		state.FollowUpOptIn = true
	})

	now := time.Now().UTC()
	sent, err := env.app.FollowUp(t.Context(), env.client, now)
	if err != nil || sent != 1 {
		t.Fatalf("FollowUp() = %d, %v; want one reminder", sent, err)
	}
	env.sent()

	old := now.Add(-25 * time.Hour)
	_, err = env.db.ExecContext(t.Context(),
		`UPDATE user_state SET last_updated = ?, last_follow_up = ? WHERE user_id = ?`, old, old, userAna,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = env.say(t, "sí")
	if err != nil {
		t.Fatal(err)
	}
	requireSent(t, env.sent(), env.text("WELCOME", welcome))
}

func TestClaimFollowUpsReleasesEarlierClaimsWhenALaterClaimCannotLock(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	for _, user := range []bot.JID{userAna, userLuis} {
		err := env.store.Turn(t.Context(), user, func(state *flow.State) ([]flow.StoredMessage, error) {
			state.CurrentNode = "WELCOME"
			state.FollowUpOptIn = true

			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC()
	_, err := env.db.ExecContext(t.Context(), `
		UPDATE user_state SET last_updated = ? WHERE user_id = ?;
		UPDATE user_state SET last_updated = ? WHERE user_id = ?;`,
		now.Add(-2*time.Minute), userAna, now.Add(-time.Minute), userLuis,
	)
	if err != nil {
		t.Fatal(err)
	}

	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- env.store.Turn(t.Context(), userLuis, func(*flow.State) ([]flow.StoredMessage, error) {
			close(locked)
			<-release

			return nil, nil
		})
	}()
	<-locked

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = env.store.ClaimFollowUps(ctx, now, 24*time.Hour)
	close(release)
	if waitErr := <-done; waitErr != nil {
		t.Fatal(waitErr)
	}
	if err == nil {
		t.Fatal("ClaimFollowUps() error = nil, want the locked later claim to fail")
	}

	requireLastFollowUpNull(t, env, userAna, true)
}

func TestReturningUserKeepsTheirChoiceWhileConversationRestarts(t *testing.T) {
	t.Parallel()

	env := start(t, withFlow(engagingFlow(t)))
	if err := env.say(t, "hola"); err != nil {
		t.Fatal(err)
	}
	env.sent()
	if err := env.say(t, "poesia"); err != nil {
		t.Fatal(err)
	}
	env.sent()
	env.age(t)

	if err := env.say(t, "buenas tardes"); err != nil {
		t.Fatal(err)
	}
	replies := env.sent()
	if len(replies) != 1 || !strings.Contains(replies[0], "Qué gusto verte de nuevo") {
		t.Fatalf("returning greeting = %q, want welcome back", replies)
	}
	if state := env.state(t); state.LastChoice != "POETRY" {
		t.Errorf("returning LastChoice = %q, want POETRY", state.LastChoice)
	}
}
