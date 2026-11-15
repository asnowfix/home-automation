package myhome

import "time"

// Heating levels, as keyed in the temperature levels map (heater.js getTemperatureLevels).
const (
	HeatingLevelAway    = "away"
	HeatingLevelComfort = "comfort"
	HeatingLevelEco     = "eco"
)

// inTimeRange mirrors heater.js isComfortTime (lines 821-844) and rooms.isInTimeRange:
// the end is exclusive and a range whose end is before its start crosses midnight.
func inTimeRange(minutes int, r TemperatureTimeRange) bool {
	if r.End < r.Start {
		return minutes >= r.Start || minutes < r.End
	}
	return minutes >= r.Start && minutes < r.End
}

// atMinutes returns the instant at the given minutes-since-midnight on now's date,
// plus dayOffset days, in now's location.
func atMinutes(now time.Time, minutes, dayOffset int) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+dayOffset, minutes/60, minutes%60, 0, 0, now.Location())
}

// HeatingTarget says which temperature level heater.js applies at now, and until when.
//
// It mirrors the target selection of heater.js (lines 1153-1162): not occupied means
// "away", otherwise inside a comfort range means "comfort", otherwise "eco". As in
// heater.js, a nil occupied (no presence data) counts as not occupied.
//
// until is when the level is next expected to change according to ranges (today's
// ranges only): the end of the current comfort range, or the start of the next range
// for eco. It is nil when unknown, notably for "away", which ends with presence rather
// than with the clock. Adjacent ranges are not merged.
//
// ok is false when levels has no value for the selected level; callers render "--".
func HeatingTarget(levels map[string]float64, ranges []TemperatureTimeRange, occupied *bool, now time.Time) (level string, celsius float64, until *time.Time, ok bool) {
	minutes := now.Hour()*60 + now.Minute()

	if occupied == nil || !*occupied {
		level = HeatingLevelAway
	} else {
		level = HeatingLevelEco
		for _, r := range ranges {
			if !inTimeRange(minutes, r) {
				continue
			}
			level = HeatingLevelComfort
			offset := 0
			if r.End < r.Start && minutes >= r.Start {
				offset = 1 // the range ends tomorrow morning
			}
			u := atMinutes(now, r.End, offset)
			until = &u
			break
		}
		if level == HeatingLevelEco {
			next := -1
			for _, r := range ranges {
				if r.Start > minutes && (next < 0 || r.Start < next) {
					next = r.Start
				}
			}
			if next >= 0 {
				u := atMinutes(now, next, 0)
				until = &u
			}
		}
	}

	celsius, ok = levels[level]
	if !ok {
		return level, 0, nil, false
	}
	return level, celsius, until, true
}
