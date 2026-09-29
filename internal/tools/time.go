package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CurrentTime tells the model what time it is — models have no
// clock, and "today", file dates, and logs all depend on it.
// Read-Only tier: knowing the time changes nothing.
type CurrentTime struct{}

func (CurrentTime) Name() string { return "current_time" }

func (CurrentTime) Description() string {
	return "Get the current date and time (ISO 8601, UTC and local) and the weekday. Use it whenever 'today', deadlines, or file dates matter."
}

func (CurrentTime) Parameters() json.RawMessage { return json.RawMessage(`{}`) }

func (CurrentTime) Tier() Tier { return TierReadOnly }

func (CurrentTime) Execute(ctx context.Context, args string) (string, error) {
	now := time.Now()
	zone, offset := now.Zone()
	return fmt.Sprintf("%s (UTC: %s, local: %s UTC%+03d%02d, weekday: %s)",
		now.Format(time.RFC1123), now.UTC().Format(time.RFC3339),
		zone, offset/3600, offset%3600/60, now.Weekday()), nil
}
