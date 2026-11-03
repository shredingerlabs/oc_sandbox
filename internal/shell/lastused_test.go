package shell

import (
	"testing"
	"time"
)

// Tests anchor at a frozen top-of-hour clock (2026-11-02 13:00 local).
func fixedNow() time.Time {
	return time.Date(2026, 11, 2, 13, 0, 0, 0, time.Local)
}

func stamp(y, m, d, hh, mm int) string {
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.UTC).Format("2006-01-02T15:04:05Z")
}

func TestRelativeLastUsed(t *testing.T) {
	now := fixedNow()
	cases := []struct {
		name  string
		stamp string // relative to now when ymd is 0
		want  string
	}{
		{"same day", stamp(2026, 11, 2, 13, 0), "Today " + fixedNow().Format("15:04")},
		{"yesterday", stamp(2026, 11, 1, 9, 0), "Yesterday"},
		{"three days ago", stamp(2026, 10, 30, 20, 0), "3 d ago"},
		{"six days ago", stamp(2026, 10, 27, 5, 0), "6 d ago"},
		{"seven days ago", stamp(2026, 10, 26, 5, 0), "2026-10-26"},
	}
	for _, tc := range cases {
		if got := RelativeLastUsed(tc.stamp, now); got != tc.want {
			t.Errorf("%s: RelativeLastUsed = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestRelativeLastUsedFallbacks(t *testing.T) {
	now := fixedNow()
	for name, stamp := range map[string]string{"empty": "", "garbage": "not-a-stamp"} {
		if got := RelativeLastUsed(stamp, now); got != "" {
			t.Errorf("%s: RelativeLastUsed = %q, want empty fallback", name, got)
		}
	}
}
