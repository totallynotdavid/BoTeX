//nolint:goconst // Nodes, names and texts repeat across cases; literals keep each case readable.
package flow_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
	"github.com/totallynotdavid/botkit/internal/flow"
	"github.com/totallynotdavid/botkit/internal/sqlite"
)

const (
	phoneAna         = "51900000001"
	userAna  bot.JID = phoneAna + "@s.whatsapp.net"
	userLuis bot.JID = "51900000002@s.whatsapp.net"
)

// waitLimit bounds every wait in the concurrency tests, so a store that
// deadlocks fails a test instead of hanging the run.
const waitLimit = 5 * time.Second

func newStore(t *testing.T) *flow.Store {
	t.Helper()

	database, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "flow.db"), sqlite.WithoutSync())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	store, err := flow.NewStore(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}

	return store
}

var errDeclined = errors.New("the flow declined the message")

// moveTo runs a turn that places user at node and stores messages.
func moveTo(t *testing.T, store *flow.Store, user bot.JID, node string, messages ...flow.StoredMessage) error {
	t.Helper()

	//nolint:wrapcheck // The helper hands Turn's error to the test as it is.
	return store.Turn(t.Context(), user, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = node

		return messages, nil
	})
}

func inbound(content string) flow.StoredMessage {
	return flow.StoredMessage{Timestamp: time.Now(), Direction: flow.Inbound, Content: content, NodeID: "START"}
}

func TestTurnStartsANewUserFresh(t *testing.T) {
	t.Parallel()

	store := newStore(t)

	err := store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		if *state != (flow.State{UserID: userAna}) {
			t.Errorf("state of a new user = %+v, want only the user ID set", state)
		}

		return nil, nil
	})
	if err != nil {
		t.Fatalf("Turn() error = %v, want none for a new user", err)
	}
}

func TestTurnSavesTheState(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	want := flow.State{
		UserID:             userAna,
		CurrentNode:        "PAYMENT",
		UserName:           "Ana",
		CourseInterest:     "advanced",
		SelectedCourseID:   "COURSE_B",
		ConsultedPrice:     true,
		VoucherPath:        "/vouchers/a.jpeg",
		RequiresHumanAgent: true,
		RepromptCount:      2,
	}

	before := time.Now()

	err := store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		*state = want

		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	after := time.Now()

	got, err := store.Load(t.Context(), userAna)
	if err != nil {
		t.Fatal(err)
	}

	if got.LastUpdated.Before(before) || got.LastUpdated.After(after) {
		t.Errorf("LastUpdated = %v, want between %v and %v", got.LastUpdated, before, after)
	}

	if got.LastUpdated.Location() != time.UTC {
		t.Errorf("LastUpdated is in %v, want UTC", got.LastUpdated.Location())
	}

	want.LastUpdated = got.LastUpdated
	if *got != want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}

	other, err := store.Load(t.Context(), userLuis)
	if err != nil {
		t.Fatal(err)
	}

	if other.CurrentNode != "" {
		t.Errorf("another user's state = %+v, want a new user", other)
	}
}

func TestNextTurnSeesTheSavedState(t *testing.T) {
	t.Parallel()

	store := newStore(t)

	err := store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = "START"
		state.UserName = "Ana"
		state.RequiresHumanAgent = true

		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		if state.CurrentNode != "START" || state.UserName != "Ana" || !state.RequiresHumanAgent {
			t.Errorf("the next turn saw %+v, want the first turn's state", state)
		}

		*state = flow.State{CurrentNode: "MENU"}

		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Load(t.Context(), userAna)
	if err != nil {
		t.Fatal(err)
	}

	if got.CurrentNode != "MENU" || got.UserName != "" || got.RequiresHumanAgent {
		t.Errorf("Load() = %+v, want the second turn's state alone", got)
	}
}

func TestTurnStoresTheStateUnderItsUser(t *testing.T) {
	t.Parallel()

	store := newStore(t)

	err := store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.UserID = userLuis
		state.CurrentNode = "MENU"

		return []flow.StoredMessage{inbound("hola")}, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	assertStored(t, store, userAna, "MENU", 1)
	assertStored(t, store, userLuis, "", 0)
}

func TestTurnStoresTheStateAndMessagesInOneTransaction(t *testing.T) {
	t.Parallel()

	received := inbound("hola")
	sent := flow.StoredMessage{Timestamp: time.Now(), Direction: flow.Outbound, Content: "bienvenida", NodeID: "MENU"}
	invalid := flow.StoredMessage{Timestamp: time.Now(), Direction: "sideways", Content: "x", NodeID: "MENU"}

	tests := []struct {
		name     string
		messages []flow.StoredMessage
		wantErr  bool
		// wantNode is the node stored for the user, empty when nothing was.
		wantNode string
		// wantCount is the size of the history after the turn.
		wantCount int
	}{
		{"state only", nil, false, "MENU", 0},
		{"a received message", []flow.StoredMessage{received}, false, "MENU", 1},
		{"a received and a sent message", []flow.StoredMessage{received, sent}, false, "MENU", 2},
		{"the last insert fails", []flow.StoredMessage{received, invalid}, true, "", 0},
		{"the first insert fails", []flow.StoredMessage{invalid, sent}, true, "", 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := newStore(t)

			err := moveTo(t, store, userAna, "MENU", test.messages...)
			if (err != nil) != test.wantErr {
				t.Fatalf("Turn() error = %v, want error: %t", err, test.wantErr)
			}

			assertStored(t, store, userAna, test.wantNode, test.wantCount)
		})
	}
}

// assertStored checks the node and the history size stored for user.
func assertStored(t *testing.T, store *flow.Store, user bot.JID, wantNode string, wantCount int) {
	t.Helper()

	got, err := store.Load(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}

	if got.CurrentNode != wantNode {
		t.Errorf("CurrentNode of %s = %q, want %q", user, got.CurrentNode, wantNode)
	}

	count, err := store.MessageCount(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}

	if count != wantCount {
		t.Errorf("MessageCount(%s) = %d, want %d", user, count, wantCount)
	}
}

func TestFailedTurnStoresNothing(t *testing.T) {
	t.Parallel()

	invalid := flow.StoredMessage{Timestamp: time.Now(), Direction: "sideways", NodeID: "MENU"}

	tests := []struct {
		name string
		fn   func(state *flow.State) ([]flow.StoredMessage, error)
		want error
	}{
		{"fn fails", func(state *flow.State) ([]flow.StoredMessage, error) {
			state.CurrentNode = "MENU"
			state.UserName = "Changed"

			return []flow.StoredMessage{inbound("lost")}, errDeclined
		}, errDeclined},
		{"the save fails", func(state *flow.State) ([]flow.StoredMessage, error) {
			state.CurrentNode = "MENU"
			state.UserName = "Changed"

			return []flow.StoredMessage{inbound("lost"), invalid}, nil
		}, nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := newStore(t)

			err := moveTo(t, store, userAna, "START", inbound("hola"))
			if err != nil {
				t.Fatal(err)
			}

			stored, err := store.Load(t.Context(), userAna)
			if err != nil {
				t.Fatal(err)
			}

			err = store.Turn(t.Context(), userAna, test.fn)
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) {
				t.Fatalf("Turn() error = %v, want an error matching %v", err, test.want)
			}

			assertUnchanged(t, store, stored)
		})
	}
}

func TestFailedSaveLeavesTheRetainedStateUnstamped(t *testing.T) {
	t.Parallel()

	invalid := flow.StoredMessage{Timestamp: time.Now(), Direction: "sideways", NodeID: "MENU"}

	tests := []struct {
		name string
		// fail makes the save of the turn fail, from inside the turn.
		fail func(cancel context.CancelFunc) []flow.StoredMessage
	}{
		{"an insert fails", func(context.CancelFunc) []flow.StoredMessage {
			return []flow.StoredMessage{inbound("lost"), invalid}
		}},
		{"the transaction cannot begin", func(cancel context.CancelFunc) []flow.StoredMessage {
			cancel()

			return nil
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := newStore(t)

			err := moveTo(t, store, userAna, "START", inbound("hola"))
			if err != nil {
				t.Fatal(err)
			}

			stored, err := store.Load(t.Context(), userAna)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var retained *flow.State

			err = store.Turn(ctx, userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
				retained = state
				state.CurrentNode = "MENU"

				return test.fail(cancel), nil
			})
			if err == nil {
				t.Fatal("Turn() succeeded, want the save to fail")
			}

			if !retained.LastUpdated.Equal(stored.LastUpdated) {
				t.Errorf("retained LastUpdated = %v, want %v as before the turn", retained.LastUpdated, stored.LastUpdated)
			}
		})
	}
}

// assertUnchanged checks that the state stored for userAna is want, down to
// LastUpdated, which only a saved turn moves, and that the history holds the
// one message stored before.
func assertUnchanged(t *testing.T, store *flow.Store, want *flow.State) {
	t.Helper()

	got, err := store.Load(t.Context(), userAna)
	if err != nil {
		t.Fatal(err)
	}

	if *got != *want {
		t.Errorf("stored state = %+v, want it as before the turn, %+v", got, want)
	}

	assertStored(t, store, userAna, "START", 1)
}

func TestConcurrentTurnsOfOneUserAllLand(t *testing.T) {
	t.Parallel()

	const turns = 20

	store := newStore(t)

	var (
		running atomic.Int32
		group   sync.WaitGroup
	)

	for range turns {
		group.Go(func() {
			err := store.Turn(t.Context(), userAna, func(state *flow.State) ([]flow.StoredMessage, error) {
				if running.Add(1) != 1 {
					t.Error("two turns of one user ran at the same time")
				}

				defer running.Add(-1)

				// Yield between the read and the write, where a lost update would happen.
				time.Sleep(time.Millisecond)

				state.RepromptCount++

				return []flow.StoredMessage{inbound("hola")}, nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}

	group.Wait()

	got, err := store.Load(t.Context(), userAna)
	if err != nil {
		t.Fatal(err)
	}

	if got.RepromptCount != turns {
		t.Errorf("RepromptCount = %d, want %d: a turn was lost", got.RepromptCount, turns)
	}

	assertStored(t, store, userAna, "", turns)

	if locks := store.Locks(); locks != 0 {
		t.Errorf("Locks() = %d after every turn ended, want 0", locks)
	}
}

func TestTurnsOfDifferentUsersDoNotBlockEachOther(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- store.Turn(t.Context(), userAna, func(*flow.State) ([]flow.StoredMessage, error) {
			close(started)
			<-release

			return nil, nil
		})
	}()

	<-started

	ctx, cancel := context.WithTimeout(t.Context(), waitLimit)
	defer cancel()

	// Ana's turn is still running, so this one only finishes if Luis does not wait for her.
	err := store.Turn(ctx, userLuis, func(state *flow.State) ([]flow.StoredMessage, error) {
		state.CurrentNode = "MENU"

		return nil, nil
	})

	close(release)

	if err != nil {
		t.Fatalf("Turn(Luis) while Ana's turn runs: %v", err)
	}

	err = <-done
	if err != nil {
		t.Fatalf("Turn(Ana) error = %v", err)
	}

	assertStored(t, store, userLuis, "MENU", 0)
}

func TestTurnWaitsForTheLockUntilItsContextEnds(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- store.Turn(t.Context(), userAna, func(*flow.State) ([]flow.StoredMessage, error) {
			close(started)
			<-release

			return nil, nil
		})
	}()

	<-started

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	err := store.Turn(ctx, userAna, func(*flow.State) ([]flow.StoredMessage, error) {
		t.Error("a turn ran while another turn of the same user held the lock")

		return nil, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Turn() error = %v, want the context's deadline", err)
	}

	close(release)

	err = <-done
	if err != nil {
		t.Fatalf("first Turn() error = %v", err)
	}

	if locks := store.Locks(); locks != 0 {
		t.Errorf("Locks() = %d after every turn ended, want 0", locks)
	}
}

func TestMessageCountIsPerUser(t *testing.T) {
	t.Parallel()

	store := newStore(t)

	turns := []struct {
		user     bot.JID
		messages int
	}{{userAna, 3}, {userLuis, 1}}

	for _, turn := range turns {
		messages := make([]flow.StoredMessage, turn.messages)
		for i := range messages {
			messages[i] = inbound("hola")
		}

		err := moveTo(t, store, turn.user, "START", messages...)
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, want := range turns {
		got, err := store.MessageCount(t.Context(), want.user)
		if err != nil {
			t.Fatal(err)
		}

		if got != want.messages {
			t.Errorf("MessageCount(%s) = %d, want %d", want.user, got, want.messages)
		}
	}

	none, err := store.MessageCount(t.Context(), "51900000003@s.whatsapp.net")
	if err != nil || none != 0 {
		t.Errorf("MessageCount(unknown user) = %d, %v; want 0", none, err)
	}
}

func TestNewStoreKeepsWhatIsStored(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "flow.db")

	database, err := sqlite.Open(t.Context(), path, sqlite.WithoutSync())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})

	first, err := flow.NewStore(t.Context(), database)
	if err != nil {
		t.Fatal(err)
	}

	err = moveTo(t, first, userAna, "MENU")
	if err != nil {
		t.Fatal(err)
	}

	second, err := flow.NewStore(t.Context(), database)
	if err != nil {
		t.Fatalf("NewStore() on an existing schema error = %v", err)
	}

	got, err := second.Load(t.Context(), userAna)
	if err != nil || got.CurrentNode != "MENU" {
		t.Errorf("Load() = %+v, %v; want the state saved before", got, err)
	}
}
