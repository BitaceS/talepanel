package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// nextCronTime computes the next time a standard 5-field cron expression
// (minute hour day-of-month month day-of-week) matches, strictly after `after`.
//
// Supported syntax per field: "*", "*/n" step, "a-b" range, "a,b,c" list, and
// plain values. Day-of-week is 0-6 with 0=Sunday (7 also accepted as Sunday).
// When both day-of-month and day-of-week are restricted, a day matches if
// EITHER matches (Vixie cron semantics).
func nextCronTime(expr string, after time.Time) (time.Time, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("cron expression must have 5 fields, got %d", len(fields))
	}

	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, fmt.Errorf("minute: %w", err)
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, fmt.Errorf("hour: %w", err)
	}
	dom, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return time.Time{}, fmt.Errorf("day-of-month: %w", err)
	}
	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return time.Time{}, fmt.Errorf("month: %w", err)
	}
	dow, err := parseCronField(fields[4], 0, 7)
	if err != nil {
		return time.Time{}, fmt.Errorf("day-of-week: %w", err)
	}
	// Normalise Sunday: accept 7, always include 0.
	if dow[7] {
		dow[0] = true
	}

	domRestricted := fields[2] != "*"
	dowRestricted := fields[4] != "*"

	// Start at the next whole minute after `after`.
	t := after.Truncate(time.Minute).Add(time.Minute)

	// Bound the search to just over a year so a never-matching expression fails
	// rather than looping forever.
	limit := after.Add(367 * 24 * time.Hour)
	for t.Before(limit) {
		if !month[int(t.Month())] {
			t = t.Add(time.Minute)
			continue
		}
		if !dayMatches(t, dom, dow, domRestricted, dowRestricted) {
			t = t.Add(time.Minute)
			continue
		}
		if hour[t.Hour()] && minute[t.Minute()] {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no matching time within a year for %q", expr)
}

func dayMatches(t time.Time, dom, dow map[int]bool, domRestricted, dowRestricted bool) bool {
	dMatch := dom[t.Day()]
	wMatch := dow[int(t.Weekday())]
	switch {
	case domRestricted && dowRestricted:
		return dMatch || wMatch
	case domRestricted:
		return dMatch
	case dowRestricted:
		return wMatch
	default:
		return true
	}
}

// parseCronField expands one cron field into a set of allowed values in [min,max].
func parseCronField(field string, min, max int) (map[int]bool, error) {
	out := make(map[int]bool)
	for _, part := range strings.Split(field, ",") {
		step := 1
		rangePart := part
		if slash := strings.Index(part, "/"); slash != -1 {
			s, err := strconv.Atoi(part[slash+1:])
			if err != nil || s < 1 {
				return nil, fmt.Errorf("invalid step in %q", part)
			}
			step = s
			rangePart = part[:slash]
		}

		lo, hi := min, max
		if rangePart != "*" {
			if dash := strings.Index(rangePart, "-"); dash != -1 {
				a, err1 := strconv.Atoi(rangePart[:dash])
				b, err2 := strconv.Atoi(rangePart[dash+1:])
				if err1 != nil || err2 != nil {
					return nil, fmt.Errorf("invalid range %q", rangePart)
				}
				lo, hi = a, b
			} else {
				v, err := strconv.Atoi(rangePart)
				if err != nil {
					return nil, fmt.Errorf("invalid value %q", rangePart)
				}
				lo, hi = v, v
			}
		}

		if lo < min || hi > max || lo > hi {
			return nil, fmt.Errorf("value out of range [%d,%d] in %q", min, max, part)
		}
		for v := lo; v <= hi; v += step {
			out[v] = true
		}
	}
	return out, nil
}
