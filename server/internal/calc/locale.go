package calc

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// Locale is how a solution writes numbers, dates and times as text. FileMaker
// takes it from the file's settings, so a French file turns 1.5 into "1,5" and
// 24 November 2026 into "24/11/2026" when a formula joins them to text. The
// formula itself is always written with "." as the decimal point.
type Locale struct {
	DecimalSeparator   string
	ThousandsSeparator string
	DateOrder          string // "YMD", "DMY" or "MDY"
	DateSeparator      string
	TimeSeparator      string
	// FileMakerNumbers writes numbers as FileMaker does: ",5" with no
	// leading zero, and "1,0E+20" for a power of ten too long to write out.
	// File4Base's own ISO locale writes "0.5" and every digit.
	FileMakerNumbers bool
}

// ISOLocale is the default for solutions created in File4Base: dates read and
// write as 2026-11-24, which sorts and exchanges without ambiguity.
var ISOLocale = Locale{DecimalSeparator: ".", DateOrder: "YMD", DateSeparator: "-", TimeSeparator: ":"}

// LocaleFor returns the settings FileMaker uses for a locale such as "fr-FR"
// or "en-US"; ok is false for one it does not know.
func LocaleFor(tag string) (Locale, bool) {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-")) {
	case "", "iso":
		return ISOLocale, true
	case "en-us":
		return Locale{DecimalSeparator: ".", ThousandsSeparator: ",", DateOrder: "MDY", DateSeparator: "/", TimeSeparator: ":", FileMakerNumbers: true}, true
	case "en-gb":
		return Locale{DecimalSeparator: ".", ThousandsSeparator: ",", DateOrder: "DMY", DateSeparator: "/", TimeSeparator: ":", FileMakerNumbers: true}, true
	case "fr-fr", "fr-be", "fr-ch", "fr-ca", "fr":
		return Locale{DecimalSeparator: ",", ThousandsSeparator: " ", DateOrder: "DMY", DateSeparator: "/", TimeSeparator: ":", FileMakerNumbers: true}, true
	case "de-de", "de":
		return Locale{DecimalSeparator: ",", ThousandsSeparator: ".", DateOrder: "DMY", DateSeparator: ".", TimeSeparator: ":", FileMakerNumbers: true}, true
	case "es-es", "it-it", "pt-pt", "nl-nl":
		return Locale{DecimalSeparator: ",", ThousandsSeparator: ".", DateOrder: "DMY", DateSeparator: "/", TimeSeparator: ":", FileMakerNumbers: true}, true
	}
	return Locale{}, false
}

// decimal is the character that separates a number's decimals in text.
func (l *Locale) decimal() rune {
	l = l.orISO()
	if l.DecimalSeparator == "" {
		return '.'
	}
	return []rune(l.DecimalSeparator)[0]
}

func (l *Locale) orISO() *Locale {
	if l == nil || l.DateOrder == "" {
		return &ISOLocale
	}
	return l
}

// formatNumber writes a number as text. Like FileMaker, it rounds to 16
// decimal places, or to the places SetPrecision asked for, and drops
// trailing zeros.
func (l *Locale) formatNumber(n decimal.Decimal, places int32) string {
	l = l.orISO()
	if l.FileMakerNumbers {
		if s, ok := scientific(n, l.DecimalSeparator); ok {
			return s
		}
	}
	n = n.Round(max(places, precision))
	if n.IsZero() {
		return "0"
	}
	s := n.StringFixed(max(-n.Exponent(), 0))
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if l.FileMakerNumbers {
		// FileMaker writes ,5 for 0,5.
		if strings.HasPrefix(s, "0.") {
			s = s[1:]
		} else if strings.HasPrefix(s, "-0.") {
			s = "-" + s[2:]
		}
	}
	if l.DecimalSeparator != "." {
		s = strings.Replace(s, ".", l.DecimalSeparator, 1)
	}
	return s
}

// scientific writes a power of ten whose zeros would be too many to write out,
// as FileMaker does: 1e20 is "1,0E+20" and 1e-12 "1,0E-12", while
// 123456789012345678901 and 2^100 are written in full.
func scientific(n decimal.Decimal, sep string) (string, bool) {
	abs := n.Abs()
	coef := abs.Coefficient()
	exp := abs.Exponent()
	// Normalise to coefficient without trailing zeros.
	ten := big.NewInt(10)
	for coef.Sign() > 0 {
		q, r := new(big.Int).QuoRem(coef, ten, new(big.Int))
		if r.Sign() != 0 {
			break
		}
		coef, exp = q, exp+1
	}
	if coef.Cmp(big.NewInt(1)) != 0 || (exp < scientificFrom && exp > -scientificBelow) {
		return "", false
	}
	sign := ""
	if n.IsNegative() {
		sign = "-"
	}
	return fmt.Sprintf("%s1%s0E%+03d", sign, sep, exp), true
}

// scientificFrom is the smallest power of ten FileMaker writes with an
// exponent.
const scientificFrom = 20

// scientificBelow is the smallest negative power of ten FileMaker writes
// with an exponent: 1e-12 is "1,0E-12".
const scientificBelow = 12

func (l *Locale) formatDate(day int64) string {
	l = l.orISO()
	y, m, d := civil(day)
	sep := l.DateSeparator
	switch l.DateOrder {
	case "DMY":
		return fmt.Sprintf("%02d%s%02d%s%04d", d, sep, m, sep, y)
	case "MDY":
		return fmt.Sprintf("%d%s%d%s%04d", m, sep, d, sep, y)
	default:
		return fmt.Sprintf("%04d%s%02d%s%02d", y, sep, m, sep, d)
	}
}

// formatTime writes a time of day, or a duration past 24 hours, as
// hours:minutes:seconds with any fraction of a second kept.
func (l *Locale) formatTime(secs decimal.Decimal) string {
	l = l.orISO()
	sign := ""
	if secs.IsNegative() {
		sign = "-"
		secs = secs.Neg()
	}
	whole := secs.Floor()
	frac := secs.Sub(whole)
	total := whole.IntPart()
	s := fmt.Sprintf("%s%02d%s%02d%s%02d", sign, total/3600, l.TimeSeparator, total%3600/60, l.TimeSeparator, total%60)
	if !frac.IsZero() {
		s += l.DecimalSeparator + strings.TrimPrefix(frac.String(), "0.")
	}
	return s
}

// formatTimestamp writes a date and a time of day; like FileMaker, it leaves
// out the seconds when there are none: "24/11/2026 13:45".
func (l *Locale) formatTimestamp(secs decimal.Decimal) string {
	whole := secs.Floor()
	day := floorDiv(whole.IntPart(), secondsPerDay) + 1
	rest := secs.Sub(decimal.NewFromInt((day - 1) * secondsPerDay))
	t := l.formatTime(rest)
	if rest.Mod(decimal.NewFromInt(60)).IsZero() {
		t = t[:strings.LastIndex(t, l.orISO().TimeSeparator)]
	}
	return l.formatDate(day) + " " + t
}

// parseDate reads a date written in the locale's order, or in ISO form, which
// is how File4Base stores dates.
func (l *Locale) parseDate(s string) (int64, bool) { return l.parseDateIn(s, 0) }

// parseDateIn is parseDate where a date written without its year, such as
// "24/11", falls in year; 0 refuses it. A two-digit year falls within 70
// years before and 30 years after year, or after 2000 when year is 0.
func (l *Locale) parseDateIn(s string, year int) (int64, bool) {
	l = l.orISO()
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if t, ok := parseISO(s); ok {
		return t, true
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '-' || r == '.' || r == ' ' })
	if len(parts) == 2 && year > 0 {
		if l.DateOrder == "YMD" {
			parts = append([]string{strconv.Itoa(year)}, parts...)
		} else {
			parts = append(parts, strconv.Itoa(year))
		}
	}
	if len(parts) != 3 {
		return 0, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		nums[i] = n
	}
	var y, m, d int
	switch l.DateOrder {
	case "DMY":
		d, m, y = nums[0], nums[1], nums[2]
	case "MDY":
		m, d, y = nums[0], nums[1], nums[2]
	default:
		y, m, d = nums[0], nums[1], nums[2]
	}
	if yearText := parts[map[string]int{"DMY": 2, "MDY": 2}[l.DateOrder]]; len(yearText) <= 2 {
		y = twoDigitYear(y, year)
	}
	return validDate(y, m, d)
}

// validDate is the day number of a real calendar date: FileMaker refuses
// 31/02/2026 rather than moving it to March.
func validDate(y, m, d int) (int64, bool) {
	if m < 1 || m > 12 || d < 1 || y < 1 || y > 4000 {
		return 0, false
	}
	day := dayNumber(y, m, d)
	if _, mm, dd := civil(day); int(mm) != m || int(dd) != d {
		return 0, false
	}
	return day, true
}

func twoDigitYear(yy, year int) int {
	if year == 0 {
		return 2000 + yy
	}
	y := year - year%100 + yy
	switch {
	case y > year+30:
		y -= 100
	case y <= year-70:
		y += 100
	}
	return y
}

func parseISO(s string) (int64, bool) {
	if len(s) < 10 || s[4] != '-' || s[7] != '-' {
		return 0, false
	}
	y, err1 := strconv.Atoi(s[0:4])
	m, err2 := strconv.Atoi(s[5:7])
	d, err3 := strconv.Atoi(s[8:10])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	return validDate(y, m, d)
}

// parseTime reads hours[:minutes[:seconds[.fraction]]], hours possibly past 24.
func (l *Locale) parseTime(s string) (decimal.Decimal, bool) {
	l = l.orISO()
	s = strings.TrimSpace(s)
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return decimal.Zero, false
	}
	if len(parts) == 2 && strings.Contains(parts[1], l.DecimalSeparator) {
		parts = append([]string{"0"}, parts...) // FileMaker reads 13:45,5 as minutes and seconds
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || m < 0 || m > 59 {
		return decimal.Zero, false
	}
	secs := decimal.NewFromInt(int64(h*3600 + m*60))
	if len(parts) == 3 {
		sec, err := decimal.NewFromString(strings.Replace(parts[2], l.DecimalSeparator, ".", 1))
		if err != nil || sec.IsNegative() || sec.GreaterThanOrEqual(decimal.NewFromInt(60)) {
			return decimal.Zero, false
		}
		secs = secs.Add(sec)
	}
	if negative {
		secs = secs.Neg()
	}
	return secs, true
}

func (l *Locale) parseTimestamp(s string) (decimal.Decimal, bool) {
	s = strings.TrimSpace(strings.Replace(s, "T", " ", 1))
	s = strings.TrimSuffix(s, "Z")
	datePart, timePart, _ := strings.Cut(s, " ")
	day, ok := l.parseDate(datePart)
	if !ok {
		return decimal.Zero, false
	}
	base := decimal.NewFromInt((day - 1) * secondsPerDay)
	if strings.TrimSpace(timePart) == "" {
		return base, true
	}
	if i := strings.IndexAny(timePart, "+"); i > 0 {
		timePart = timePart[:i] // a stored UTC offset; File4Base keeps wall time
	}
	secs, ok := l.parseTime(timePart)
	if !ok || secs.IsNegative() || secs.GreaterThanOrEqual(decimal.NewFromInt(secondsPerDay)) {
		return decimal.Zero, false // a timestamp's time is a time of day
	}
	return base.Add(secs), true
}
