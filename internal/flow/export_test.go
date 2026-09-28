package flow

import (
	"context"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// Render reaches the unexported render from the external tests.
func Render(text string, data map[string]string) string { return render(text, data) }

// Load reads a state without a turn, so tests can look at what a turn stored.
func (s *Store) Load(ctx context.Context, user bot.JID) (*State, error) { return s.load(ctx, user) }

// Locks is how many users hold or wait for a lock.
func (s *Store) Locks() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.locks)
}
