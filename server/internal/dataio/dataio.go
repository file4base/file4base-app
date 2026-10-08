// Package dataio reads record data out of the file formats other programs
// write — comma- and tab-separated text, Excel workbooks and XML — so a data
// set can be brought into File4Base without going through the REST API by
// hand (#38).
//
// Everything is read as **text**. Turning text into a number or a date is the
// import's job, not the parser's, so a value that will not convert can be
// reported with the row and column it came from rather than quietly becoming
// zero.
package dataio

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidSource is returned when a file cannot be read as the format it
// claims to be.
var ErrInvalidSource = errors.New("invalid source file")

// ErrSourceTooLarge is returned when a file is past the guards below. The
// MessagePack path has msgpackguard; text and spreadsheets need their own.
var ErrSourceTooLarge = errors.New("source file too large")

// Limits are the guards every parser applies. A file past any of them is
// refused before it is read into memory rather than after.
type Limits struct {
	MaxBytes   int
	MaxRows    int
	MaxColumns int
}

// DefaultLimits are what the import endpoints use.
func DefaultLimits() Limits {
	return Limits{
		MaxBytes:   25 << 20, // 25 MiB
		MaxRows:    100000,
		MaxColumns: 256,
	}
}

func (l Limits) orDefaults() Limits {
	d := DefaultLimits()
	if l.MaxBytes <= 0 {
		l.MaxBytes = d.MaxBytes
	}
	if l.MaxRows <= 0 {
		l.MaxRows = d.MaxRows
	}
	if l.MaxColumns <= 0 {
		l.MaxColumns = d.MaxColumns
	}
	return l
}

// Format is the kind of file being read.
type Format string

const (
	FormatCSV  Format = "csv"
	FormatTSV  Format = "tsv"
	FormatXLSX Format = "xlsx"
	FormatXML  Format = "xml"
)

// Formats are the kinds File4Base reads, in the order the import dialog
// offers them.
var Formats = []Format{FormatCSV, FormatTSV, FormatXLSX, FormatXML}

// FormatFromName guesses the format from a file name. An unknown extension
// reads as comma-separated text, which is what most exports are.
func FormatFromName(name string) Format {
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".tsv"),
		strings.HasSuffix(strings.ToLower(name), ".tab"),
		strings.HasSuffix(strings.ToLower(name), ".txt"):
		return FormatTSV
	case strings.HasSuffix(strings.ToLower(name), ".xlsx"),
		strings.HasSuffix(strings.ToLower(name), ".xlsm"):
		return FormatXLSX
	case strings.HasSuffix(strings.ToLower(name), ".xml"):
		return FormatXML
	default:
		return FormatCSV
	}
}

// Options control how a source is read.
type Options struct {
	Format Format

	// The first row holds the column names rather than data.
	HasHeader bool

	// Delimited text: the character between fields. Empty means the parser
	// works it out from the first line.
	Delimiter string

	// Excel: which sheet. Empty means the first one.
	Sheet string

	// XML: the element that repeats, one per record. Empty means the parser
	// works it out.
	RecordElement string

	Limits Limits
}

// Table is a source read into memory: what its columns are called and the
// rows underneath them, all as text.
type Table struct {
	// Column names. Without a header row these are "Column 1", "Column 2"…
	Columns []string `json:"columns"`

	Rows [][]string `json:"rows"`

	// Sheets of an Excel workbook, so the dialog can offer the others.
	Sheets []string `json:"sheets,omitempty"`

	// True when the file held more rows than the limit allowed. The rows that
	// were read are still returned, and the caller says so rather than
	// importing part of a file silently.
	Truncated bool `json:"truncated"`
}

// Cell is the value at a position, or "" when the row is short. A source row
// need not have a value for every column.
func (t *Table) Cell(row, column int) string {
	if row < 0 || row >= len(t.Rows) {
		return ""
	}
	cells := t.Rows[row]
	if column < 0 || column >= len(cells) {
		return ""
	}
	return cells[column]
}

// Sample returns at most n rows, for a preview that shows what will be
// imported without reading the lot into a dialog.
func (t *Table) Sample(n int) [][]string {
	if n <= 0 || len(t.Rows) <= n {
		return t.Rows
	}
	return t.Rows[:n]
}

// Parse reads a source in whichever format the options name.
func Parse(data []byte, opts Options) (*Table, error) {
	opts.Limits = opts.Limits.orDefaults()
	if len(data) > opts.Limits.MaxBytes {
		return nil, fmt.Errorf("%w: %d bytes, and the limit is %d",
			ErrSourceTooLarge, len(data), opts.Limits.MaxBytes)
	}

	switch opts.Format {
	case FormatXLSX:
		return parseXLSX(data, opts)
	case FormatXML:
		return parseXML(data, opts)
	case FormatTSV:
		if opts.Delimiter == "" {
			opts.Delimiter = "\t"
		}
		return parseDelimited(data, opts)
	default:
		return parseDelimited(data, opts)
	}
}

// headerOrNumbered turns the first row into column names, or makes them up.
func headerOrNumbered(rows [][]string, hasHeader bool, limits Limits) ([]string, [][]string, error) {
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width > limits.MaxColumns {
		return nil, nil, fmt.Errorf("%w: %d columns, and the limit is %d",
			ErrSourceTooLarge, width, limits.MaxColumns)
	}

	if !hasHeader {
		columns := make([]string, width)
		for i := range columns {
			columns[i] = fmt.Sprintf("Column %d", i+1)
		}
		return columns, rows, nil
	}
	if len(rows) == 0 {
		return []string{}, nil, nil
	}

	columns := make([]string, width)
	for i := range columns {
		name := ""
		if i < len(rows[0]) {
			name = strings.TrimSpace(rows[0][i])
		}
		if name == "" {
			// A header cell left blank still names a column, so the rest do
			// not shift up by one.
			name = fmt.Sprintf("Column %d", i+1)
		}
		columns[i] = name
	}
	return columns, rows[1:], nil
}
