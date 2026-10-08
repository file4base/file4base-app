package dataio

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// delimiterCandidates are tried when the caller did not say which one the file
// uses, in the order they are most likely.
var delimiterCandidates = []rune{',', '\t', ';', '|'}

// detectDelimiter picks the character that splits the first few lines into the
// most columns, consistently. A file whose first line has three commas and no
// tabs is comma-separated; one with neither is a single column, which is also
// an answer.
func detectDelimiter(text string) rune {
	lines := make([]string, 0, 5)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) == 5 {
			break
		}
	}
	if len(lines) == 0 {
		return ','
	}

	best, bestScore := ',', 0
	for _, candidate := range delimiterCandidates {
		first := strings.Count(lines[0], string(candidate))
		if first == 0 {
			continue
		}
		// The same count on every line is what makes it the separator rather
		// than a character that happens to appear in the data.
		consistent := true
		for _, line := range lines[1:] {
			if strings.Count(line, string(candidate)) != first {
				consistent = false
				break
			}
		}
		score := first
		if consistent {
			score += 1000
		}
		if score > bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

// stripBOM removes the byte order mark a Windows export leaves at the start,
// which would otherwise become part of the first column's name.
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

func parseDelimited(data []byte, opts Options) (*Table, error) {
	data = stripBOM(data)
	text := string(data)

	delimiter := ','
	if opts.Delimiter != "" {
		r, size := utf8.DecodeRuneInString(opts.Delimiter)
		if r == utf8.RuneError && size <= 1 {
			return nil, fmt.Errorf("%w: %q is not a usable separator", ErrInvalidSource, opts.Delimiter)
		}
		delimiter = r
	} else {
		delimiter = detectDelimiter(text)
	}

	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = delimiter
	// Rows of different lengths are normal in exported data; a short row is a
	// row with empty cells, not a broken file.
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = false

	rows := make([][]string, 0, 64)
	truncated := false
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidSource, err)
		}
		// A line of nothing but separators is a blank line, not a record.
		if len(record) == 0 || (len(record) == 1 && strings.TrimSpace(record[0]) == "") {
			continue
		}
		if len(rows) >= opts.Limits.MaxRows {
			truncated = true
			break
		}
		rows = append(rows, record)
	}

	columns, body, err := headerOrNumbered(rows, opts.HasHeader, opts.Limits)
	if err != nil {
		return nil, err
	}
	return &Table{Columns: columns, Rows: body, Truncated: truncated}, nil
}
