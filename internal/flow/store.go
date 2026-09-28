package flow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/totallynotdavid/botkit/internal/bot"
)

const schema = `
CREATE TABLE IF NOT EXISTS user_state (
    user_id TEXT PRIMARY KEY,
    current_node TEXT NOT NULL DEFAULT '',
    user_name TEXT NOT NULL DEFAULT '',
    course_interest TEXT NOT NULL DEFAULT '',
    selected_course_id TEXT NOT NULL DEFAULT '',
    consulted_price BOOLEAN NOT NULL DEFAULT FALSE,
    voucher_path TEXT NOT NULL DEFAULT '',
    requires_human_agent BOOLEAN NOT NULL DEFAULT FALSE,
    reprompt_count INTEGER NOT NULL DEFAULT 0,
    last_updated DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_user_state_updated ON user_state(last_updated);

CREATE TABLE IF NOT EXISTS conversation_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    direction TEXT NOT NULL CHECK(direction IN ('inbound', 'outbound')),
    message_content TEXT,
    node_id TEXT
);

CREATE INDEX IF NOT EXISTS idx_conversation_user_time ON conversation_history(user_id, timestamp DESC);
`

const (
	loadStateQuery = `
		SELECT current_node, user_name, course_interest, selected_course_id, consulted_price,
		       voucher_path, requires_human_agent, reprompt_count, last_updated
		FROM user_state WHERE user_id = ?
	`

	saveStateQuery = `
		INSERT INTO user_state (user_id, current_node, user_name, course_interest, selected_course_id,
		                        consulted_price, voucher_path, requires_human_agent, reprompt_count, last_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			current_node = excluded.current_node,
			user_name = excluded.user_name,
			course_interest = excluded.course_interest,
			selected_course_id = excluded.selected_course_id,
			consulted_price = excluded.consulted_price,
			voucher_path = excluded.voucher_path,
			requires_human_agent = excluded.requires_human_agent,
			reprompt_count = excluded.reprompt_count,
			last_updated = excluded.last_updated
	`

	saveMessageQuery = `
		INSERT INTO conversation_history (user_id, timestamp, direction, message_content, node_id)
		VALUES (?, ?, ?, ?, ?)
	`

	countMessagesQuery = `SELECT COUNT(id) FROM conversation_history WHERE user_id = ?`
)

// Store keeps every user's State and conversation history in the shared
// SQLite database. It is the only writer of both tables, and Turn is the only
// way to change a State.
type Store struct {
	db *sql.DB

	mu sync.Mutex
	// locks holds a lock for each user with a turn running or waiting, and no
	// other, so it does not grow with the number of users the bot has met.
	locks map[bot.JID]*userLock
}

// userLock serializes the turns of one user. It is a channel rather than a
// sync.Mutex so that waiting for it can be cancelled.
type userLock struct {
	held chan struct{}
	// turns counts the turns holding or waiting for this lock. Store.mu guards it.
	turns int
}

// NewStore creates the store's tables if they do not exist.
func NewStore(ctx context.Context, database *sql.DB) (*Store, error) {
	_, err := database.ExecContext(ctx, schema)
	if err != nil {
		return nil, fmt.Errorf("exec schema: %w", err)
	}

	return &Store{db: database, locks: make(map[bot.JID]*userLock)}, nil
}

// Turn runs one turn of user's conversation: it loads their state, or a new
// one for a user with none, and calls turn with it. When turn succeeds, Turn
// saves the state turn left and the messages it returned in one transaction,
// and sets LastUpdated to the current time in UTC. When turn or the save
// fails, nothing is stored, and the error is returned.
//
// Turns of one user run one at a time, in the order they get the lock, so turn
// may send replies and they stay in order. Turns of different users run in
// parallel. The lock is in this process: two processes on one database are not
// supported. turn must not start a turn of the same user.
func (s *Store) Turn(ctx context.Context, user bot.JID, turn func(state *State) ([]StoredMessage, error)) error {
	lock, err := s.acquire(ctx, user)
	if err != nil {
		return err
	}

	defer s.release(user, lock)

	state, err := s.load(ctx, user)
	if err != nil {
		return err
	}

	messages, err := turn(state)
	if err != nil {
		return fmt.Errorf("turn of %s: %w", user, err)
	}

	return s.save(ctx, user, state, messages)
}

// MessageCount returns how many messages, sent and received, user's history
// holds.
func (s *Store) MessageCount(ctx context.Context, user bot.JID) (int, error) {
	var count int

	err := s.db.QueryRowContext(ctx, countMessagesQuery, user).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count messages of %s: %w", user, err)
	}

	return count, nil
}

// acquire takes user's lock, waiting for the turn that holds it. It gives up
// when ctx is done.
func (s *Store) acquire(ctx context.Context, user bot.JID) (*userLock, error) {
	s.mu.Lock()

	lock := s.locks[user]
	if lock == nil {
		lock = &userLock{held: make(chan struct{}, 1)}
		s.locks[user] = lock
	}

	lock.turns++

	s.mu.Unlock()

	select {
	case lock.held <- struct{}{}:
		return lock, nil
	case <-ctx.Done():
		s.forget(user, lock)

		return nil, fmt.Errorf("wait for turn of %s: %w", user, context.Cause(ctx))
	}
}

func (s *Store) release(user bot.JID, lock *userLock) {
	<-lock.held

	s.forget(user, lock)
}

// forget ends one turn's interest in lock and drops it when no turn is left.
func (s *Store) forget(user bot.JID, lock *userLock) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lock.turns--
	if lock.turns == 0 {
		delete(s.locks, user)
	}
}

// load returns the state of user. A user with none stored yet is new, not an
// error: they get an empty State.
func (s *Store) load(ctx context.Context, user bot.JID) (*State, error) {
	state := &State{UserID: user}

	err := s.db.QueryRowContext(ctx, loadStateQuery, user).Scan(
		&state.CurrentNode,
		&state.UserName,
		&state.CourseInterest,
		&state.SelectedCourseID,
		&state.ConsultedPrice,
		&state.VoucherPath,
		&state.RequiresHumanAgent,
		&state.RepromptCount,
		&state.LastUpdated,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}

	if err != nil {
		return nil, fmt.Errorf("load state of %s: %w", user, err)
	}

	return state, nil
}

// save stores state and appends messages to user's history in one transaction:
// either all of them are stored or none is. state gets its new LastUpdated only
// once the commit succeeded.
func (s *Store) save(ctx context.Context, user bot.JID, state *State, messages []StoredMessage) error {
	saved := *state
	saved.LastUpdated = time.Now().UTC()

	txn, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save of %s: %w", user, err)
	}

	err = saveTurn(ctx, txn, user, &saved, messages)
	if err != nil {
		return fmt.Errorf("save state of %s: %w", user, errors.Join(err, txn.Rollback()))
	}

	err = txn.Commit()
	if err != nil {
		return fmt.Errorf("commit save of %s: %w", user, err)
	}

	state.LastUpdated = saved.LastUpdated

	return nil
}

func saveTurn(ctx context.Context, txn *sql.Tx, user bot.JID, state *State, messages []StoredMessage) error {
	_, err := txn.ExecContext(ctx, saveStateQuery,
		user,
		state.CurrentNode,
		state.UserName,
		state.CourseInterest,
		state.SelectedCourseID,
		state.ConsultedPrice,
		state.VoucherPath,
		state.RequiresHumanAgent,
		state.RepromptCount,
		state.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("state: %w", err)
	}

	for _, message := range messages {
		_, err = txn.ExecContext(ctx, saveMessageQuery,
			user, message.Timestamp, message.Direction, message.Content, message.NodeID,
		)
		if err != nil {
			return fmt.Errorf("%s message: %w", message.Direction, err)
		}
	}

	return nil
}
