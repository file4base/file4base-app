package migration

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/data"
	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/file4base/file4base-app/server/internal/sourcetext"
)

// sourceTextKey is the reserved row key a migrated row carries its original
// texts under; the data import keeps them (package sourcetext).
const sourceTextKey = sourcetext.Key

// Permissive source values (#124).
//
// A FileMaker Number field can hold "12-A", "(4x2)" or "N/A", a Date field
// text that is no date, a Time field a duration past 24 hours. FileMaker keeps
// the text and computes with its own reading of it. When the migration request
// names the source file's locale (source_locale), such a value is migrated
// rather than refused: the typed column gets FileMaker's interpretation and
// the original text is kept beside it (package sourcetext). Without a locale
// the migration stays strict and refuses them, because how "7,125" reads
// depends on it.

// sourceLocale is how a FileMaker file writes numbers and dates.
type sourceLocale struct {
	Tag       string
	Decimal   rune
	DateOrder data.DateOrder
}

// sourceLocales are the locales a migration request may name. A tag outside
// this list is refused rather than guessed.
var sourceLocales = map[string]sourceLocale{
	"en-us": {Tag: "en-US", Decimal: '.', DateOrder: data.DateMDY},
	"en-gb": {Tag: "en-GB", Decimal: '.', DateOrder: data.DateDMY},
	"en-ie": {Tag: "en-IE", Decimal: '.', DateOrder: data.DateDMY},
	"en-au": {Tag: "en-AU", Decimal: '.', DateOrder: data.DateDMY},
	"en-nz": {Tag: "en-NZ", Decimal: '.', DateOrder: data.DateDMY},
	"fr-fr": {Tag: "fr-FR", Decimal: ',', DateOrder: data.DateDMY},
	"fr-be": {Tag: "fr-BE", Decimal: ',', DateOrder: data.DateDMY},
	"es-es": {Tag: "es-ES", Decimal: ',', DateOrder: data.DateDMY},
	"ca-es": {Tag: "ca-ES", Decimal: ',', DateOrder: data.DateDMY},
	"de-de": {Tag: "de-DE", Decimal: ',', DateOrder: data.DateDMY},
	"de-at": {Tag: "de-AT", Decimal: ',', DateOrder: data.DateDMY},
	"it-it": {Tag: "it-IT", Decimal: ',', DateOrder: data.DateDMY},
	"pt-pt": {Tag: "pt-PT", Decimal: ',', DateOrder: data.DateDMY},
	"pt-br": {Tag: "pt-BR", Decimal: ',', DateOrder: data.DateDMY},
	"nl-nl": {Tag: "nl-NL", Decimal: ',', DateOrder: data.DateDMY},
	"nl-be": {Tag: "nl-BE", Decimal: ',', DateOrder: data.DateDMY},
}

// SupportedSourceLocales lists the locale tags a migration request may name.
func SupportedSourceLocales() []string {
	tags := make([]string, 0, len(sourceLocales))
	for _, loc := range sourceLocales {
		tags = append(tags, loc.Tag)
	}
	sort.Strings(tags)
	return tags
}

// parseSourceLocale reads source_locale. Empty means none was named.
func parseSourceLocale(raw string) (*sourceLocale, error) {
	tag := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "_", "-"))
	if tag == "" {
		return nil, nil
	}
	loc, ok := sourceLocales[tag]
	if !ok {
		return nil, fmt.Errorf("%w: source_locale %q is not supported; use one of %s",
			ErrInvalidMigrationXML, raw, strings.Join(SupportedSourceLocales(), ", "))
	}
	return &loc, nil
}

// reCommaDecimalLiteral is a plain decimal written with a decimal comma.
var reCommaDecimalLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:,[0-9]*)?|,[0-9]+)$`)

// fileMakerNumber reads text the way FileMaker's GetAsNumber does, given the
// source file's decimal separator, and returns the number as a plain decimal
// ("-42", "7.125"). It returns false when the text holds no digit at all,
// which FileMaker reads as empty.
//
// Observed on FileMaker Pro 26.0.2 with French settings
// (testdata/claris26-fr-scalars): every character other than a digit and the
// decimal separator is ignored ("12-A" is 12, "1/2" is 12, "12-34" is 1234,
// "7.125" is 7125); a minus sign or an opening parenthesis before the first
// digit makes the number negative ("-3a7" is -37, "(4x2)" is -42); text without
// a digit is empty; the number is exact, not rounded to a float
// ("9007199254740993,125"). Not observed, and chosen here: a second decimal
// separator is ignored like any other character.
func fileMakerNumber(text string, decimal rune) (string, bool) {
	var whole, fraction strings.Builder
	negative, digits, inFraction := false, false, false
	for _, r := range text {
		switch {
		case r >= '0' && r <= '9':
			digits = true
			if inFraction {
				fraction.WriteRune(r)
			} else {
				whole.WriteRune(r)
			}
		case r == decimal:
			inFraction = true
		case (r == '-' || r == '(') && !digits:
			negative = true
		}
	}
	if !digits {
		return "", false
	}
	w := strings.TrimLeft(whole.String(), "0")
	if w == "" {
		w = "0"
	}
	f := fraction.String()
	out := w
	if f != "" {
		out += "." + f
	}
	if negative && strings.Trim(w+f, "0") != "" {
		out = "-" + out
	}
	return out, true
}

// localeNumberLiteral reports whether text is a plain decimal as the source
// locale writes one, and returns it in File4Base's notation. Such a value is
// what it says: it needs no original text kept beside it.
func localeNumberLiteral(text string, loc *sourceLocale) (string, bool) {
	if loc.Decimal == ',' {
		if !reCommaDecimalLiteral.MatchString(text) {
			return "", false
		}
		literal := strings.Replace(text, ",", ".", 1)
		if strings.HasPrefix(literal, ".") || strings.HasPrefix(literal, "-.") || strings.HasPrefix(literal, "+.") {
			literal = strings.Replace(literal, ".", "0.", 1)
		}
		if strings.HasSuffix(literal, ".") {
			literal = strings.TrimSuffix(literal, ".")
		}
		return literal, true
	}
	if !reSourceNumericLiteral.MatchString(text) {
		return "", false
	}
	return text, true
}

// errSourceRepetitions refuses a value of several repetitions in a typed
// field: each repetition is its own value, which a single column cannot hold.
var errSourceRepetitions = errors.New("holds several repetitions, which a single typed field cannot keep")

// interpretSourceValue migrates one value of a typed source field given the
// source locale. It returns the typed value (nil when FileMaker reads the
// text as empty or invalid) and the original text to keep beside it, which is
// empty when the typed value says exactly what the source said.
//
// kind is the migration's field type: NUMBER, DATE or TIMESTAMP (a FileMaker
// Time field is migrated as a TIMESTAMP). dateOrder is the order of a native
// date; FMPXMLRESULT writes its dates month first whatever the locale.
func interpretSourceValue(text, kind string, loc *sourceLocale, dateOrder data.DateOrder) (interface{}, string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, "", nil
	}
	if strings.ContainsRune(text, '\x1d') {
		return nil, "", errSourceRepetitions
	}
	switch kind {
	case "NUMBER":
		if literal, ok := localeNumberLiteral(text, loc); ok {
			return literal, "", nil
		}
		if number, ok := fileMakerNumber(text, loc.Decimal); ok {
			return number, text, nil
		}
		return nil, text, nil
	case "DATE":
		if value, err := data.CoerceImportValue(dbal.FieldTypeDate, text, dateOrder); err == nil {
			return value, "", nil
		}
		return nil, text, nil
	case "TIMESTAMP":
		if value, err := coerceNativeValue(text, "TIMESTAMP", string(dateOrder)); err == nil {
			return value, "", nil
		}
		// A Time field holds a time of day, which File4Base keeps in a
		// TIMESTAMP column. A duration past 24 hours or a negative one is no
		// time of day: it keeps its text and has no typed value.
		// A fraction of a second follows a comma where the decimal
		// separator is one ("15:04:05,25").
		trimmed := strings.Replace(strings.TrimSpace(text), ",", ".", 1)
		for _, layout := range []string{"15:04:05", "15:04"} {
			if at, err := time.Parse(layout, trimmed); err == nil {
				return timeOfDay(at), "", nil
			}
		}
		return nil, text, nil
	}
	value, err := coerceSourceValue(text, kind)
	return value, "", err
}

// timeOfDay writes a time of day read on its own, which Go places on 1
// January of year 0, as a timestamp on 1 January 0001: where the calculation
// engine and FileMaker place one, and a day every engine stores. A fraction
// of a second is kept.
func timeOfDay(at time.Time) string {
	return time.Date(1, time.January, 1, at.Hour(), at.Minute(), at.Second(), at.Nanosecond(), time.UTC).Format(time.RFC3339Nano)
}

// countSourceTexts counts the values of migrated rows kept with their
// original text, and the fields they are in.
func countSourceTexts(rows []map[string]interface{}) (int, []string) {
	total := 0
	fields := map[string]struct{}{}
	for _, row := range rows {
		texts, _ := row[sourceTextKey].(map[string]string)
		total += len(texts)
		for field := range texts {
			fields[field] = struct{}{}
		}
	}
	names := make([]string, 0, len(fields))
	for f := range fields {
		names = append(names, f)
	}
	sort.Strings(names)
	return total, names
}

// addSourceText records the original text of one value of a migrated row.
func addSourceText(row map[string]interface{}, field, text string) {
	texts, _ := row[sourceTextKey].(map[string]string)
	if texts == nil {
		texts = map[string]string{}
		row[sourceTextKey] = texts
	}
	texts[field] = text
}

// reportSourceTexts names, in the report, the values of a dataset migrated
// with their original text.
func reportSourceTexts(report *ConversionReport, source, table string, rows []map[string]interface{}, loc *sourceLocale) {
	total, fields := countSourceTexts(rows)
	if total == 0 || loc == nil {
		return
	}
	report.Counts.SourceTextValues += total
	addReportItem(report, ReportItem{
		Category:   CategoryTranslated,
		Domain:     "data",
		SourceName: source,
		TargetName: table,
		Detail: fmt.Sprintf("%d value(s) of %s kept with their original text: each field holds FileMaker's %s reading "+
			"of its text (empty where FileMaker reads none), which sorting, finds and calculations use, and the record "+
			"shows the text as it was written", total, strings.Join(fields, ", "), loc.Tag),
	})
}
