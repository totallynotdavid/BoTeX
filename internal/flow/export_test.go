package flow

import (
	"context"
	"fmt"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// Render reaches the unexported render from the external tests.
func Render(text string, data map[string]string) string { return render(text, data) }

// Load reads a state without a turn, so tests can look at what a turn stored.
func (s *Store) Load(ctx context.Context, user bot.JID) (*State, error) { return s.load(ctx, user) }

// History returns the messages stored for user, oldest first.
func (s *Store) History(ctx context.Context, user bot.JID) (history []StoredMessage, err error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT timestamp, direction, message_content, node_id FROM conversation_history WHERE user_id = ? ORDER BY id`, user)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}

	defer func() {
		closeErr := rows.Close()
		if closeErr != nil && err == nil {
			err = fmt.Errorf("close history: %w", closeErr)
		}
	}()

	for rows.Next() {
		var message StoredMessage

		err = rows.Scan(&message.Timestamp, &message.Direction, &message.Content, &message.NodeID)
		if err != nil {
			return nil, fmt.Errorf("scan history: %w", err)
		}

		history = append(history, message)
	}

	return history, rows.Err() //nolint:wrapcheck // a test helper hands the driver's error on.
}

// Locks is how many users hold or wait for a lock.
func (s *Store) Locks() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.locks)
}
