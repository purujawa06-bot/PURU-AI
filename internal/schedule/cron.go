// Cron expression parsing and next-match computation for scheduled tasks.
//
// Supported format is standard 5-field cron: "minute hour dom month dow".
// Each field accepts "*", "*/step", "a,b,c", "a-b", and "a-b/step".
// Month is 1-12, weekday is 0-6 with Sunday as 0 (7 is normalized to 0).
// Names like "mon" are rejected to keep the parser small and predictable.
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronField struct {
	min  int
	max  int
	vals map[int]bool
}

type cronSchedule struct {
	minute cronField
	hour   cronField
	dom    cronField
	month  cronField
	dow    cronField
}

func parseCron(expr string) (cronSchedule, error) {
	var empty cronSchedule
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return empty, fmt.Errorf("cron_expr must have 5 fields (minute hour dom month dow), got %q", expr)
	}
	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return empty, fmt.Errorf("cron minute: %w", err)
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return empty, fmt.Errorf("cron hour: %w", err)
	}
	dom, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return empty, fmt.Errorf("cron day-of-month: %w", err)
	}
	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return empty, fmt.Errorf("cron month: %w", err)
	}
	dow, err := parseCronField(fields[4], 0, 7)
	if err != nil {
		return empty, fmt.Errorf("cron weekday: %w", err)
	}
	// Normalize Sunday 7 to 0.
	if dow.vals[7] {
		dow.vals[0] = true
		delete(dow.vals, 7)
	}
	return cronSchedule{minute: minute, hour: hour, dom: dom, month: month, dow: dow}, nil
}

func parseCronField(raw string, min, max int) (cronField, error) {
	out := cronField{min: min, max: max, vals: map[int]bool{}}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return out, fmt.Errorf("empty field")
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return out, fmt.Errorf("empty list item in %q", raw)
		}
		step := 1
		base := part
		if idx := strings.Index(part, "/"); idx >= 0 {
			base = strings.TrimSpace(part[:idx])
			stepRaw := strings.TrimSpace(part[idx+1:])
			n, err := strconv.Atoi(stepRaw)
			if err != nil || n <= 0 {
				return out, fmt.Errorf("bad step in %q", part)
			}
			step = n
		}
		lo, hi := min, max
		if base == "" || base == "*" {
			// Full range with step.
		} else if idx := strings.Index(base, "-"); idx >= 0 {
			loRaw := strings.TrimSpace(base[:idx])
			hiRaw := strings.TrimSpace(base[idx+1:])
			loVal, err := strconv.Atoi(loRaw)
			if err != nil {
				return out, fmt.Errorf("bad range in %q", part)
			}
			hiVal, err := strconv.Atoi(hiRaw)
			if err != nil {
				return out, fmt.Errorf("bad range in %q", part)
			}
			lo, hi = loVal, hiVal
		} else {
			val, err := strconv.Atoi(base)
			if err != nil {
				return out, fmt.Errorf("bad value %q", part)
			}
			lo, hi = val, val
		}
		if lo < min || hi > max || lo > hi {
			return out, fmt.Errorf("value out of range %d-%d in %q", min, max, part)
		}
		for v := lo; v <= hi; v += step {
			out.vals[v] = true
		}
	}
	if len(out.vals) == 0 {
		return out, fmt.Errorf("no values in %q", raw)
	}
	return out, nil
}

func (c cronSchedule) matches(t time.Time) bool {
	if !c.minute.vals[t.Minute()] {
		return false
	}
	if !c.hour.vals[t.Hour()] {
		return false
	}
	if !c.month.vals[int(t.Month())] {
		return false
	}
	domMatch := c.dom.vals[t.Day()]
	dowMatch := c.dow.vals[int(t.Weekday())]
	domWild := len(c.dom.vals) == c.dom.max-c.dom.min+1
	dowWild := len(c.dow.vals) == 7
	// Standard cron semantics: when both dom and dow are restricted,
	// either may trigger. When one is wildcard, the other must match.
	switch {
	case domWild && dowWild:
		return true
	case domWild:
		return dowMatch
	case dowWild:
		return domMatch
	default:
		return domMatch || dowMatch
	}
}

// NextCronAfter returns the next minute-aligned time strictly after now.
func NextCronAfter(expr string, loc *time.Location, after time.Time) (time.Time, error) {
	sched, err := parseCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		loc = time.UTC
	}
	if after.IsZero() {
		after = time.Now()
	}
	// Start at the next minute boundary in the target location.
	candidate := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		if sched.matches(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no cron match within one year for %q", expr)
}
