package bot

import "time"

// SetGrace shortens how long Run waits for handlers after a stop.
func (b *Bot) SetGrace(d time.Duration) { b.grace = d }
