package calc

import (
	"math/big"
	"strings"

	"github.com/shopspring/decimal"
)

type bigInt = big.Int

func conversionFunctions() map[string]function {
	return map[string]function{
		"GETASTEXT": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			if a[0].isEmpty() {
				return empty, nil
			}
			return textValue(a[0].asText(ev.loc)), nil
		}),
		// GetContainerAttribute ( container ; "filename" | "fileSize" ):
		// what is known of the file without reading it. Any other attribute
		// is empty.
		"GETCONTAINERATTRIBUTE": fixed(2, func(ev *evaluator, _ callNode, a []value) (value, error) {
			box := a[0].box
			if box == nil {
				return empty, nil
			}
			switch strings.ToLower(strings.TrimSpace(a[1].asText(ev.loc))) {
			case "filename":
				return textValue(box.Filename), nil
			case "filesize":
				if box.Size <= 0 {
					return empty, nil
				}
				return intValue(box.Size), nil
			}
			return empty, nil
		}),
		"GETASNUMBER": fixed(1, func(ev *evaluator, _ callNode, a []value) (value, error) {
			n, ok := a[0].numberOrEmpty(ev.loc)
			if !ok {
				return empty, nil
			}
			return numberValue(n), nil
		}),
		"GETASDATE": fixed(1, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].kind == kindText {
				if strings.TrimSpace(a[0].text) == "" {
					return empty, nil
				}
				day, ok := ev.loc.parseDateIn(a[0].text, ev.now.Year())
				if !ok {
					// "01/02/2026 13:00" is the date of that timestamp.
					secs, isTimestamp := ev.loc.parseTimestamp(a[0].text)
					if !isTimestamp {
						return empty, errorAt(c.at, "%q is not a date", a[0].text)
					}
					day = floorDiv(secs.Floor().IntPart(), secondsPerDay) + 1
				}
				return checkedDate(c.at, day)
			}
			day, ok := a[0].asDay(ev.loc)
			if !ok {
				return empty, nil
			}
			return checkedDate(c.at, day)
		}),
		"GETASTIME": fixed(1, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].isEmpty() || a[0].kind == kindText && strings.TrimSpace(a[0].text) == "" {
				return empty, nil
			}
			if a[0].kind == kindDate {
				// A date is the time since 1 January 0001: FileMaker's
				// GetAsTime ( Date ( 11 ; 24 ; 2026 ) ) is 17758632:00:00.
				return timeOfDayValue(decimal.NewFromInt((a[0].num.IntPart() - 1) * secondsPerDay)), nil
			}
			secs, ok := a[0].asSeconds(ev.loc)
			if !ok {
				return empty, errorAt(c.at, "%q is not a time", a[0].asText(ev.loc))
			}
			return timeOfDayValue(secs), nil
		}),
		"GETASTIMESTAMP": fixed(1, func(ev *evaluator, c callNode, a []value) (value, error) {
			if a[0].isEmpty() || a[0].kind == kindText && strings.TrimSpace(a[0].text) == "" {
				return empty, nil
			}
			secs, ok := a[0].asTimestampSeconds(ev.loc)
			if !ok {
				return empty, errorAt(c.at, "%q is not a timestamp", a[0].asText(ev.loc))
			}
			return timestampValue(secs), nil
		}),
	}
}

// formattingFunctions are FileMaker's character-style functions. File4Base
// text has no character styles, so they return the text itself: a formula
// that colours a word still gives the right characters.
func formattingFunctions() map[string]function {
	plain := func(minArgs, maxArgs int) function {
		return between(minArgs, maxArgs, func(ev *evaluator, _ callNode, a []value) (value, error) {
			return textValue(a[0].asText(ev.loc)), nil
		})
	}
	out := map[string]function{}
	for _, name := range []string{"TEXTFORMATREMOVE", "TEXTCOLORREMOVE", "TEXTFONTREMOVE", "TEXTSIZEREMOVE", "TEXTSTYLEREMOVE"} {
		out[name] = plain(1, -1)
	}
	for _, name := range []string{"TEXTCOLOR", "TEXTSIZE", "TEXTSTYLEADD", "TEXTFONT"} {
		out[name] = plain(2, -1)
	}
	out["RGB"] = fixed(3, func(ev *evaluator, _ callNode, a []value) (value, error) {
		r, g, b := a[0].asNumber(ev.loc).IntPart(), a[1].asNumber(ev.loc).IntPart(), a[2].asNumber(ev.loc).IntPart()
		return intValue(r*65536 + g*256 + b), nil
	})
	return out
}
