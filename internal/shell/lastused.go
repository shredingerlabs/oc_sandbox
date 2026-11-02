package shell

import (
	"strconv"
	"time"
)

// lastUsedLayout is the registry's last_used stamp format (UTC).
const lastUsedLayout = "2006-01-02T15:04:05Z"

// RelativeLastUsed renders a projects.json last_used stamp for a card
// row: same day reads "Today 13:04" (local time), the previous day
// "Yesterday", the last six days "n d ago", anything older the absolute
// local date. Empty or unparseable stamps render empty so callers can
// fall back (the card keeps showing the path).
func RelativeLastUsed(stamp string, now time.Time) string {
	if stamp == "" {
		return ""
	}
	t, err := time.ParseInLocation(lastUsedLayout, stamp, time.UTC)
	if err != nil {
		return ""
	}
	local := t.Local()
	// compare on local midnight boundaries so DST shifts don't skip days
	midnight := func(x time.Time) time.Time {
		y, m, d := x.Local().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, x.Local().Location())
	}
	days := int(midnight(now).Sub(midnight(local)).Hours() / 24)
	switch {
	case days <= 0:
		return "Today " + local.Format("15:04")
	case days == 1:
		return "Yesterday"
	case days <= 6:
		return strconv.Itoa(days) + " d ago"
	default:
		return local.Format("2006-01-02")
	}
}

