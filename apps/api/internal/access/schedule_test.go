package access

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(v int) *int { return &v }

// 2026-11-23 is a Monday.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 11, 22+day, hour, minute, 0, 0, time.UTC)
}

func TestWindowAllDay(t *testing.T) {
	w := Window{Weekdays: []int16{1, 2, 3, 4, 5}}
	assert.True(t, w.Allows(at(1, 0, 0)), "Monday 00:00")
	assert.True(t, w.Allows(at(5, 23, 59)), "Friday 23:59")
	assert.False(t, w.Allows(at(6, 12, 0)), "Saturday")
	assert.False(t, w.Allows(at(7, 12, 0)), "Sunday is ISO 7")
}

func TestWindowDaytime(t *testing.T) {
	w := Window{Weekdays: []int16{1, 2, 3, 4, 5}, Start: ptr(8 * 60), End: ptr(18 * 60)}
	assert.False(t, w.Allows(at(1, 7, 59)))
	assert.True(t, w.Allows(at(1, 8, 0)), "start is inclusive")
	assert.True(t, w.Allows(at(1, 17, 59)))
	assert.False(t, w.Allows(at(1, 18, 0)), "end is exclusive")
	assert.False(t, w.Allows(at(6, 10, 0)), "weekend")
}

func TestWindowWrapsMidnight(t *testing.T) {
	// Friday night shift 22:00-06:00.
	w := Window{Weekdays: []int16{5}, Start: ptr(22 * 60), End: ptr(6 * 60)}
	assert.True(t, w.Allows(at(5, 23, 0)), "Friday 23:00")
	assert.True(t, w.Allows(at(6, 3, 0)), "Saturday 03:00 belongs to Friday's shift")
	assert.False(t, w.Allows(at(6, 22, 30)), "Saturday night is not granted")
	assert.False(t, w.Allows(at(5, 3, 0)), "Friday 03:00 belongs to Thursday's shift")
	assert.False(t, w.Allows(at(5, 12, 0)))
}

func TestWindowSundayToMondayWrap(t *testing.T) {
	w := Window{Weekdays: []int16{7}, Start: ptr(20 * 60), End: ptr(2 * 60)}
	assert.True(t, w.Allows(at(8, 1, 0)), "Monday 01:00 after Sunday start")
}

func TestTimeOfDayRoundTrip(t *testing.T) {
	m, err := ParseTimeOfDay("08:30")
	require.NoError(t, err)
	assert.Equal(t, 510, m)
	assert.Equal(t, "08:30", FormatTimeOfDay(m))
	assert.Equal(t, 510, *pgToMinutes(minutesToPG(&m)))
	assert.Nil(t, pgToMinutes(minutesToPG(nil)))

	_, err = ParseTimeOfDay("25:00")
	assert.Error(t, err)
}
