package access

import (
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Window is the weekly time window of a grant, in the site time zone.
// Start/End are minutes after midnight; nil means all day.
type Window struct {
	Weekdays []int16 // ISO: 1 = Monday ... 7 = Sunday
	Start    *int
	End      *int
}

// isoWeekday converts time.Weekday (Sunday = 0) to ISO (Sunday = 7).
func isoWeekday(t time.Time) int16 {
	if wd := t.Weekday(); wd != time.Sunday {
		return int16(wd) // #nosec G115 -- Weekday is 1..6 here
	}
	return 7
}

// Allows reports whether local time t (already in the site zone) is inside w.
// A window whose end is before its start wraps past midnight and belongs to
// the weekday on which it started (Fri 22:00-06:00 covers Sat 03:00).
func (w Window) Allows(t time.Time) bool {
	day := isoWeekday(t)
	if w.Start == nil || w.End == nil {
		return slices.Contains(w.Weekdays, day)
	}
	m := t.Hour()*60 + t.Minute()
	start, end := *w.Start, *w.End
	if start < end {
		return slices.Contains(w.Weekdays, day) && m >= start && m < end
	}
	prev := day - 1
	if prev == 0 {
		prev = 7
	}
	return (slices.Contains(w.Weekdays, day) && m >= start) ||
		(slices.Contains(w.Weekdays, prev) && m < end)
}

// ParseTimeOfDay parses "HH:MM" into minutes after midnight.
func ParseTimeOfDay(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, fmt.Errorf("invalid time %q, want HH:MM", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// FormatTimeOfDay renders minutes after midnight as "HH:MM".
func FormatTimeOfDay(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

func minutesToPG(m *int) pgtype.Time {
	if m == nil {
		return pgtype.Time{}
	}
	return pgtype.Time{Microseconds: int64(*m) * 60_000_000, Valid: true}
}

func pgToMinutes(t pgtype.Time) *int {
	if !t.Valid {
		return nil
	}
	m := int(t.Microseconds / 60_000_000)
	return &m
}
