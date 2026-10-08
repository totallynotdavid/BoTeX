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
    last_choice TEXT NOT NULL DEFAULT '',
    follow_up_opt_in BOOLEAN NOT NULL DEFAULT FALSE,
    last_follow_up DATETIME,
    consulted_price BOOLEAN NOT NULL DEFAULT FALSE,
    voucher_path TEXT NOT NULL DEFAULT '',
    requires_human_agent BOOLEAN NOT NULL DEFAULT FALSE,
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
		SELECT current_node, user_name, course_interest, selected_course_id, last_choice,
		       follow_up_opt_in, last_follow_up, consulted_price, voucher_path,
		       requires_human_agent, last_updated
		FROM user_state WHERE user_id = ?
	`

	saveStateQuery = `
		INSERT INTO user_state (user_id, current_node, user_name, course_interest, selected_course_id,
		                        last_choice, follow_up_opt_in, last_follow_up, consulted_price,
		                        voucher_path, requires_human_agent, last_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			current_node = excluded.current_node,
			user_name = excluded.user_name,
			course_interest = excluded.course_interest,
			selected_course_id = excluded.selected_course_id,
			last_choice = excluded.last_choice,
			follow_up_opt_in = excluded.follow_up_opt_in,
			last_follow_up = excluded.last_follow_up,
			consulted_price = excluded.consulted_price,
			voucher_path = excluded.voucher_path,
			requires_human_agent = requires_human_agent OR excluded.requires_human_agent,
			last_updated = excluded.last_updated
	`

	saveMessageQuery = `
		INSERT INTO conversation_history (user_id, timestamp, direction, message_content, node_id)
		VALUES (?, ?, ?, ?, ?)
	`

	countMessagesQuery = `SELECT COUNT(id) FROM conversation_history WHERE user_id = ?`
)

// FollowUp is a claimed reminder for a user who explicitly opted in.
type FollowUp struct {
	User       bot.JID
	LastChoice string
	ClaimedAt  time.Time
}

// Store keeps every user's State and conversation history in the shared
// SQLite database. It is the only writer of both tables. Inbound turns use
// Turn; the follow-up scheduler uses state-only claims/releases and a
// history-only append.
//
// requires_human_agent has one writer in each direction. A turn can set it,
// by leaving State.RequiresHumanAgent true when it began false, and only
// ClearHandoff clears it. ClearHandoff may run in another process, outside the
// per-user lock, so no save may write the flag back as false or restore a value
// it loaded: a save writes the flag only to raise it.
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
// Turns of one user run one at a time, in the order they get the lock. Replies
// sent after Turn returns are not ordered, because the lock is already
// released. Turns of different users run in parallel. The lock is in this
// process: two processes running the bot on one database are not supported.
// turn must not start a turn of the same user.
//
// A turn raises RequiresHumanAgent by setting it. It cannot lower it. A
// State.RequiresHumanAgent that was true when turn began is not saved again,
// so a ClearHandoff that lands during the turn holds.
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

	waiting := state.RequiresHumanAgent

	messages, err := turn(state)
	if err != nil {
		return fmt.Errorf("turn of %s: %w", user, err)
	}

	return s.saveInbound(ctx, user, state, messages, state.RequiresHumanAgent && !waiting)
}

// Handoff is a user the flow handed to a person: the flow set
// RequiresHumanAgent and nobody has cleared it.
type Handoff struct {
	User bot.JID
	// Name is the name the user gave or their profile name, or empty.
	Name string
	// Node is where the conversation stands.
	Node string
	// Since is when the user last wrote.
	Since time.Time
	// LastMessage is the start of what the user last wrote, or empty.
	LastMessage string
}

// ErrNoHandoff is returned by ClearHandoff for a user who is not waiting for a
// person.
var ErrNoHandoff = errors.New("no hand-off for this user")

// Handoffs lists the users waiting for a person, the longest wait first.
func (s *Store) Handoffs(ctx context.Context) (handoffs []Handoff, returnErr error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, user_name, current_node, last_updated
		FROM user_state WHERE requires_human_agent ORDER BY last_updated, user_id`)
	if err != nil {
		return nil, fmt.Errorf("list hand-offs: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()

	for rows.Next() {
		var handoff Handoff

		err = rows.Scan(&handoff.User, &handoff.Name, &handoff.Node, &handoff.Since)
		if err != nil {
			return nil, fmt.Errorf("scan hand-off: %w", err)
		}

		handoffs = append(handoffs, handoff)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("read hand-offs: %w", err)
	}

	// The rows are closed before the history is read, so a one-connection
	// database does not wait on itself.
	for idx := range handoffs {
		handoffs[idx].LastMessage, err = s.historyHint(ctx, handoffs[idx].User)
		if err != nil {
			return nil, err
		}
	}

	return handoffs, nil
}

// ClearHandoff records that a person has taken over user, so the user leaves
// Handoffs. It changes nothing else, LastUpdated included, and is one
// statement, so it is safe beside a running bot, even from another process: a
// turn in flight cannot write the flag back. Only a later escalation by the
// flow sets it again. It returns ErrNoHandoff for a user who is not waiting.
func (s *Store) ClearHandoff(ctx context.Context, user bot.JID) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE user_state SET requires_human_agent = FALSE WHERE user_id = ? AND requires_human_agent`, user)
	if err != nil {
		return fmt.Errorf("clear hand-off of %s: %w", user, err)
	}

	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("clear hand-off of %s: %w", user, err)
	}

	if changed == 0 {
		return fmt.Errorf("%w: %s", ErrNoHandoff, user)
	}

	return nil
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

// ClaimFollowUps atomically claims reminders that are both opted in and past
// the interval. A claim is released when its send fails; a successful send
// leaves the claim as the user's last-follow-up timestamp. The write preserves
// LastUpdated, which belongs to inbound conversation activity.
func (s *Store) ClaimFollowUps(ctx context.Context, now time.Time, interval time.Duration) (claimed []FollowUp, returnErr error) {
	now = now.UTC()
	cutoff := now.Add(-interval)

	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id FROM user_state
		WHERE follow_up_opt_in AND (last_follow_up IS NULL OR last_follow_up <= ?)
		ORDER BY last_updated`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("find follow-ups: %w", err)
	}
	defer func() { returnErr = errors.Join(returnErr, rows.Close()) }()

	var users []bot.JID

	for rows.Next() {
		var user bot.JID

		err = rows.Scan(&user)
		if err != nil {
			return nil, fmt.Errorf("scan follow-up: %w", err)
		}

		users = append(users, user)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("read follow-ups: %w", err)
	}

	claimed = make([]FollowUp, 0, len(users))
	for _, user := range users {
		followUp, err := s.claimFollowUp(ctx, user, cutoff, now)
		if err != nil {
			releaseErr := s.releaseClaims(claimed, ctx)

			return nil, errors.Join(err, releaseErr)
		}

		if followUp != nil {
			claimed = append(claimed, *followUp)
		}
	}

	return claimed, nil
}

// ReleaseFollowUp returns a failed claim to the eligible pool. It only clears
// the matching timestamp, so a newer successful scheduler cannot be undone.
func (s *Store) ReleaseFollowUp(ctx context.Context, user bot.JID, claimedAt time.Time) error {
	return s.schedulerState(ctx, user, func(state *State) error {
		if state.LastFollowUp.Equal(claimedAt) {
			state.LastFollowUp = time.Time{}
		}

		return nil
	})
}

// RecordFollowUp appends a bot-initiated reminder after its send succeeds. It
// writes history only: the claim already records the send attempt and remains
// in place even if this history write fails, preventing a duplicate reminder.
func (s *Store) RecordFollowUp(ctx context.Context, user bot.JID, timestamp time.Time, text string) error {
	lock, err := s.acquire(ctx, user)
	if err != nil {
		return err
	}
	defer s.release(user, lock)

	state, err := s.load(ctx, user)
	if err != nil {
		return err
	}

	txn, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin follow-up history of %s: %w", user, err)
	}

	_, err = txn.ExecContext(ctx, saveMessageQuery, user, timestamp.UTC(), Outbound, text, state.CurrentNode)
	if err != nil {
		return fmt.Errorf("save follow-up history of %s: %w", user, errors.Join(err, txn.Rollback()))
	}

	err = txn.Commit()
	if err != nil {
		return fmt.Errorf("commit follow-up history of %s: %w", user, err)
	}

	return nil
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

	var lastFollowUp sql.NullTime

	err := s.db.QueryRowContext(ctx, loadStateQuery, user).Scan(
		&state.CurrentNode,
		&state.UserName,
		&state.CourseInterest,
		&state.SelectedCourseID,
		&state.LastChoice,
		&state.FollowUpOptIn,
		&lastFollowUp,
		&state.ConsultedPrice,
		&state.VoucherPath,
		&state.RequiresHumanAgent,
		&state.LastUpdated,
	)
	if lastFollowUp.Valid {
		state.LastFollowUp = lastFollowUp.Time
	}

	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}

	if err != nil {
		return nil, fmt.Errorf("load state of %s: %w", user, err)
	}

	return state, nil
}

// historyHint returns the most recent non-empty inbound text, bounded so an
// old message cannot turn a short reply into an accidental transcript dump.
func (s *Store) historyHint(ctx context.Context, user bot.JID) (string, error) {
	var text string

	err := s.db.QueryRowContext(ctx, `
		SELECT message_content FROM conversation_history
		WHERE user_id = ? AND direction = 'inbound' AND TRIM(message_content) <> ''
		ORDER BY id DESC LIMIT 1`, user).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}

	if err != nil {
		return "", fmt.Errorf("load history hint of %s: %w", user, err)
	}

	const maxHint = 80
	if len([]rune(text)) > maxHint {
		text = string([]rune(text)[:maxHint]) + "…"
	}

	return text, nil
}

func (s *Store) claimFollowUp(ctx context.Context, user bot.JID, cutoff, claimedAt time.Time) (*FollowUp, error) {
	var followUp *FollowUp

	err := s.schedulerState(ctx, user, func(state *State) error {
		if !state.FollowUpOptIn || (!state.LastFollowUp.IsZero() && state.LastFollowUp.After(cutoff)) {
			return nil
		}

		state.LastFollowUp = claimedAt
		followUp = &FollowUp{User: user, LastChoice: state.LastChoice, ClaimedAt: claimedAt}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return followUp, nil
}

// schedulerState changes only scheduler-owned state and preserves LastUpdated.
// The app separately treats a recent claim as activity without rewriting the
// inbound conversation clock.
func (s *Store) schedulerState(ctx context.Context, user bot.JID, change func(*State) error) error {
	lock, err := s.acquire(ctx, user)
	if err != nil {
		return err
	}
	defer s.release(user, lock)

	state, err := s.load(ctx, user)
	if err != nil {
		return err
	}

	err = change(state)
	if err != nil {
		return err
	}

	return s.saveScheduler(ctx, user, state)
}

func (s *Store) releaseClaims(claimed []FollowUp, ctx context.Context) error {
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()

	var errs []error

	for _, followUp := range claimed {
		err := s.ReleaseFollowUp(releaseCtx, followUp.User, followUp.ClaimedAt)
		if err != nil {
			errs = append(errs, fmt.Errorf("release follow-up for %s: %w", followUp.User, err))
		}
	}

	return errors.Join(errs...)
}

// saveInbound commits an inbound turn and its history atomically. It updates
// LastUpdated only after the transaction succeeds. escalated is whether the
// turn raised RequiresHumanAgent. Only then is the flag written.
func (s *Store) saveInbound(ctx context.Context, user bot.JID, state *State, messages []StoredMessage, escalated bool) error {
	saved := *state
	saved.LastUpdated = time.Now().UTC()
	saved.RequiresHumanAgent = escalated

	err := s.save(ctx, user, &saved, messages)
	if err != nil {
		return err
	}

	state.LastUpdated = saved.LastUpdated

	return nil
}

// saveScheduler commits scheduler-owned state without changing LastUpdated or
// RequiresHumanAgent.
func (s *Store) saveScheduler(ctx context.Context, user bot.JID, state *State) error {
	saved := *state
	saved.RequiresHumanAgent = false

	return s.save(ctx, user, &saved, nil)
}

// save stores state and messages in one transaction: either all of them are
// stored or none is. A true state.RequiresHumanAgent raises the stored flag. A
// false one leaves it as it is.
func (s *Store) save(ctx context.Context, user bot.JID, state *State, messages []StoredMessage) error {
	txn, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save of %s: %w", user, err)
	}

	err = saveTurn(ctx, txn, user, state, messages)
	if err != nil {
		return fmt.Errorf("save state of %s: %w", user, errors.Join(err, txn.Rollback()))
	}

	err = txn.Commit()
	if err != nil {
		return fmt.Errorf("commit save of %s: %w", user, err)
	}

	return nil
}

func saveTurn(ctx context.Context, txn *sql.Tx, user bot.JID, state *State, messages []StoredMessage) error {
	lastFollowUp := sql.NullTime{Time: state.LastFollowUp.UTC(), Valid: !state.LastFollowUp.IsZero()}

	_, err := txn.ExecContext(ctx, saveStateQuery,
		user,
		state.CurrentNode,
		state.UserName,
		state.CourseInterest,
		state.SelectedCourseID,
		state.LastChoice,
		state.FollowUpOptIn,
		lastFollowUp,
		state.ConsultedPrice,
		state.VoucherPath,
		state.RequiresHumanAgent,
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
