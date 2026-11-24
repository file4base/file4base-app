package calc

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

var (
	dayNamesEN   = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	monthNamesEN = []string{"January", "February", "March", "April", "May", "June", "July", "August",
		"September", "October", "November", "December"}
)

// weekday is 1 for Sunday through 7 for Saturday, as DayOfWeek returns.
// 1 January 0001 was a Monday.
func weekday(day int64) int64 { return floorMod(day, 7) + 1 }

func dateFunctions() map[string]function {
	dayOf := func(f func(ev *evaluator, day int64) value) function {
		return fixed(1, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			day, ok := a[0].asDay(ev.loc)
			if !ok || day < 1 {
				return empty, errorAt(c.at, "%q is not a date", a[0].asText(ev.loc))
			}
			return f(ev, day), nil
		})
	}
	secondsOf := func(f func(secs decimal.Decimal) value) function {
		return fixed(1, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			secs, ok := a[0].asSeconds(ev.loc)
			if !ok {
				return empty, errorAt(c.at, "%q is not a time", a[0].asText(ev.loc))
			}
			return f(secs), nil
		})
	}
	return map[string]function{
		"DATE": fixed(3, func(ev *evaluator, _ callNode, a []value) (value, error) {
			if a[0].isEmpty() && a[1].isEmpty() && a[2].isEmpty() {
				return empty, nil
			}
			return dateValue(dayNumber(int(a[2].asNumber(ev.loc).IntPart()), int(a[0].asNumber(ev.loc).IntPart()),
				int(a[1].asNumber(ev.loc).IntPart()))), nil
		}),
		"TIME": fixed(3, func(ev *evaluator, _ callNode, a []value) (value, error) {
			secs := a[0].asNumber(ev.loc).Mul(decimal.NewFromInt(3600)).
				Add(a[1].asNumber(ev.loc).Mul(decimal.NewFromInt(60))).Add(a[2].asNumber(ev.loc))
			return timeOfDayValue(secs), nil
		}),
		"TIMESTAMP": fixed(2, func(ev *evaluator, c callNode, a []value) (value, error) {
			day, ok := a[0].asDay(ev.loc)
			if !ok {
				return empty, errorAt(c.at, "%q is not a date", a[0].asText(ev.loc))
			}
			secs, _ := a[1].asSeconds(ev.loc)
			return timestampValue(decimal.NewFromInt((day - 1) * secondsPerDay).Add(secs)), nil
		}),
		"YEAR":      dayOf(func(_ *evaluator, d int64) value { y, _, _ := civil(d); return intValue(y) }),
		"MONTH":     dayOf(func(_ *evaluator, d int64) value { _, m, _ := civil(d); return intValue(m) }),
		"DAY":       dayOf(func(_ *evaluator, d int64) value { _, _, dd := civil(d); return intValue(dd) }),
		"DAYOFWEEK": dayOf(func(_ *evaluator, d int64) value { return intValue(weekday(d)) }),
		"DAYOFYEAR": dayOf(func(_ *evaluator, d int64) value {
			y, _, _ := civil(d)
			return intValue(d - dayNumber(int(y), 1, 1) + 1)
		}),
		"DAYNAME": dayOf(func(ev *evaluator, d int64) value {
			return textValue(ev.loc.dayName(int(weekday(d) - 1)))
		}),
		"MONTHNAME": dayOf(func(ev *evaluator, d int64) value {
			_, m, _ := civil(d)
			return textValue(ev.loc.monthName(int(m - 1)))
		}),
		"WEEKOFYEAR": dayOf(func(_ *evaluator, d int64) value {
			// FileMaker counts the week holding 1 January as week 1, weeks
			// starting on Sunday.
			y, _, _ := civil(d)
			jan1 := dayNumber(int(y), 1, 1)
			return intValue((d-jan1+weekday(jan1)-1)/7 + 1)
		}),
		"WEEKOFYEARFISCAL": fixed(2, func(ev *evaluator, c callNode, a []value) (value, error) {
			d, ok := a[0].asDay(ev.loc)
			if !ok {
				return empty, errorAt(c.at, "%q is not a date", a[0].asText(ev.loc))
			}
			start := a[1].asNumber(ev.loc).IntPart()
			if start < 1 || start > 7 {
				return empty, errorAt(c.at, "the week cannot start on day %d", start)
			}
			// Week 1 is the first with four days or more in the year, the
			// rule of ISO 8601 for any first day of the week: the week
			// belongs to the year of its fourth day.
			fourth := d - floorMod(weekday(d)-start, 7) + 3
			y, _, _ := civil(fourth)
			return intValue((fourth-dayNumber(int(y), 1, 1))/7 + 1), nil
		}),
		"HOUR": secondsOf(func(s decimal.Decimal) value {
			return numberValue(s.Div(decimal.NewFromInt(3600)).Truncate(0))
		}),
		"MINUTE": secondsOf(func(s decimal.Decimal) value {
			return numberValue(s.Mod(decimal.NewFromInt(3600)).Div(decimal.NewFromInt(60)).Floor())
		}),
		"SECONDS": secondsOf(func(s decimal.Decimal) value {
			return numberValue(s.Mod(decimal.NewFromInt(60)))
		}),
		// TODAY and NOW are File4Base's own, kept for formulas written before
		// Get ( CurrentDate ) was supported.
		"TODAY": fixed(0, func(ev *evaluator, _ callNode, _ []value) (value, error) {
			return ev.currentDate(), nil
		}),
		"NOW": fixed(0, func(ev *evaluator, _ callNode, _ []value) (value, error) {
			return ev.currentTimestamp(), nil
		}),
	}
}

func (l *Locale) dayName(i int) string {
	if l.orISO().DecimalSeparator == "," && l.DateSeparator == "/" {
		return []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}[i]
	}
	return dayNamesEN[i]
}

func (l *Locale) monthName(i int) string {
	if l.orISO().DecimalSeparator == "," && l.DateSeparator == "/" {
		return []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août",
			"septembre", "octobre", "novembre", "décembre"}[i]
	}
	return monthNamesEN[i]
}

func (ev *evaluator) currentDate() value {
	return dateValue(dayNumber(ev.now.Year(), int(ev.now.Month()), ev.now.Day()))
}

func (ev *evaluator) currentTimestamp() value {
	t := ev.now.Truncate(time.Second)
	return timestampValue(timestampSeconds(time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)))
}

// get answers Get ( Selector ): the context first, then what the engine knows
// itself — the clock and a new UUID.
func (ev *evaluator) get(t getNode) (value, error) {
	raw, err := ev.ctx.Get(t.selector)
	if err == nil {
		return fromStored(raw), nil
	}
	if !errors.Is(err, ErrNotAvailable) {
		return empty, errorAt(t.at, "Get ( %s ) failed: %v", t.selector, err)
	}
	switch t.selector {
	case "CURRENTDATE", "CURRENTHOSTDATE":
		return ev.currentDate(), nil
	case "CURRENTTIMESTAMP", "CURRENTHOSTTIMESTAMP":
		return ev.currentTimestamp(), nil
	case "CURRENTTIME", "CURRENTHOSTTIME":
		h, m, s := ev.now.Clock()
		return timeOfDayValue(decimal.NewFromInt(int64(h*3600 + m*60 + s))), nil
	case "CURRENTTIMEUTCMILLISECONDS":
		return numberValue(timestampSeconds(ev.now.UTC()).Mul(decimal.NewFromInt(1000)).Floor()), nil
	case "UUID":
		return textValue(newUUID()), nil
	case "UUIDNUMBER":
		var b [8]byte
		_, _ = rand.Read(b[:])
		return numberValue(decimal.NewFromBigInt(new(bigInt).SetBytes(b[:]), 0)), nil
	case "CALCULATIONREPETITIONNUMBER":
		return intValue(1), nil
	case "SELF":
		return empty, errorAt(t.at, "Self is only known where the formula belongs to a layout object or a field")
	case "SYSTEMPLATFORM":
		return intValue(4), nil // FileMaker's answer for a web client
	}
	return empty, errorAt(t.at, "Get ( %s ) is not available here", strings.Title(strings.ToLower(t.selector)))
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	s := strings.ToUpper(hex.EncodeToString(b[:]))
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
