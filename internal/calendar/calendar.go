// Package calendar models the NSE trading calendar. Weekends are always
// non-trading; additional exchange holidays are supplied from the DB. The pure
// Calendar type is easy to unit-test; Service wraps it with DB-backed reloads.
package calendar

import "time"

const DateKey = "2006-01-02"

// Calendar answers trading-day questions given a fixed holiday set.
type Calendar struct {
	holidays map[string]bool
}

// New builds a Calendar from a list of holiday dates.
func New(holidays []time.Time) *Calendar {
	m := make(map[string]bool, len(holidays))
	for _, h := range holidays {
		m[h.Format(DateKey)] = true
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
	return !c.holidays[d.Format(DateKey)]
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
		if !have[d.Format(DateKey)] {
			missing = append(missing, d)
		}
	}
	return missing
}
