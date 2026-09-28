package bot

import "time"

// RefusalSlots is how many refusal replies may run at once.
const RefusalSlots = refusalSlots

// SetGrace shortens how long Run waits for handlers after a stop.
func (b *Bot) SetGrace(d time.Duration) { b.grace = d }
