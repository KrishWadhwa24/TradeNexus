// Package calendar models the NSE trading calendar. Weekends are always
// non-trading; additional exchange holidays are supplied from the DB. The pure
// Calendar type is easy to unit-test; Service wraps it with DB-backed reloads.
package calendar

import "time"

const dateKey = "2006-01-02"

// Calendar answers trading-day questions given a fixed holiday set.
type Calendar struct {
	holidays map[string]bool
}

// New builds a Calendar from a list of holiday dates.
func New(holidays []time.Time) *Calendar {
	m := make(map[string]bool, len(holidays))
	for _, h := range holidays {
		m[h.Format(dateKey)] = true
	}
	return &Calendar{holidays: m}
}

// IsWeekend reports Saturday/Sunday.
func IsWeekend(d time.Time) bool {
	wd := d.Weekday()
	return wd == time.Saturday || wd == time.Sunday
}

// IsTradingDay is true when d is a weekday and not a listed holiday.
func (c *Calendar) IsTradingDay(d time.Time) bool {
	if IsWeekend(d) {
		return false
	}
	return !c.holidays[d.Format(dateKey)]
}

// IsMarketOpen reports whether NSE cash is open at time t (a trading day and
// within 09:15–15:30 local time).
func (c *Calendar) IsMarketOpen(t time.Time) bool {
	if !c.IsTradingDay(t) {
		return false
	}
	mins := t.Hour()*60 + t.Minute()
	return mins >= 9*60+15 && mins <= 15*60+30
}

// LastFinalizedTradingDay returns the most recent trading day whose session has
// fully closed. The regular close is 15:30 IST; bufferMinutes adds a grace
// period so we don't read a candle Angel is still settling right at the bell.
// Today counts only once now is past close+buffer; otherwise the previous
// trading day is returned. Weekends/holidays are skipped.
func (c *Calendar) LastFinalizedTradingDay(now time.Time, bufferMinutes int) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	closeMins := 15*60 + 30 + bufferMinutes
	nowMins := now.Hour()*60 + now.Minute()
	if c.IsTradingDay(day) && nowMins >= closeMins {
		return day
	}
	for {
		day = day.AddDate(0, 0, -1)
		if c.IsTradingDay(day) {
			return day
		}
	}
}

// TradingDays returns the trading days in [from, to] inclusive (date-only).
func (c *Calendar) TradingDays(from, to time.Time) []time.Time {
	var out []time.Time
	d := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, to.Location())
	for !d.After(end) {
		if c.IsTradingDay(d) {
			out = append(out, d)
		}
		d = d.AddDate(0, 0, 1)
	}
	return out
}

// MissingTradingDays returns trading days in (after, to] that are NOT present in
// the `have` set. `have` is keyed by "2006-01-02". This is the core of gap
// detection: it distinguishes real data gaps from weekends/holidays.
func (c *Calendar) MissingTradingDays(after, to time.Time, have map[string]bool) []time.Time {
	var missing []time.Time
	for _, d := range c.TradingDays(after.AddDate(0, 0, 1), to) {
		if !have[d.Format(dateKey)] {
			missing = append(missing, d)
		}
	}
	return missing
}
