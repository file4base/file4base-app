package dataio

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// WriteDelimited writes a table as comma- or tab-separated text, with the
// column names on the first line.
//
// The byte order mark goes at the front of a CSV so Excel opens it as UTF-8
// rather than mangling accented names, which is the usual complaint about
// exported data.
func WriteDelimited(table *Table, delimiter rune, withBOM bool) ([]byte, error) {
	var buf bytes.Buffer
	if withBOM {
		buf.Write([]byte{0xEF, 0xBB, 0xBF})
	}

	writer := csv.NewWriter(&buf)
	writer.Comma = delimiter
	if err := writer.Write(table.Columns); err != nil {
		return nil, err
	}
	for _, row := range table.Rows {
		// A short row is padded, so every line has the same number of fields.
		if len(row) < len(table.Columns) {
			padded := make([]string, len(table.Columns))
			copy(padded, row)
			row = padded
		}
		if err := writer.Write(row[:len(table.Columns)]); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteXLSX writes a table as a one-sheet Excel workbook.
func WriteXLSX(table *Table, sheetName string) ([]byte, error) {
	name := strings.TrimSpace(sheetName)
	if name == "" {
		name = "Sheet1"
	}
	// Excel refuses these in a sheet name, so they are replaced rather than
	// letting the export fail on a table called "Sales/Returns".
	for _, bad := range []string{"/", "\\", "?", "*", "[", "]", ":"} {
		name = strings.ReplaceAll(name, bad, "-")
	}
	if len(name) > 31 {
		name = name[:31]
	}

	file := excelize.NewFile()
	defer file.Close()
	if err := file.SetSheetName("Sheet1", name); err != nil {
		return nil, err
	}

	write := func(row int, values []string) error {
		for c, value := range values {
			axis, err := excelize.CoordinatesToCellName(c+1, row)
			if err != nil {
				return err
			}
			// Written as text, so a value that looks like a number or a date
			// comes back out exactly as it went in.
			if err := file.SetCellStr(name, axis, value); err != nil {
				return err
			}
		}
		return nil
	}

	if err := write(1, table.Columns); err != nil {
		return nil, fmt.Errorf("failed writing the column names: %w", err)
	}
	for i, row := range table.Rows {
		if err := write(i+2, row); err != nil {
			return nil, fmt.Errorf("failed writing row %d: %w", i+1, err)
		}
	}

	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ContentType is what a written file is served as.
func ContentType(format Format) string {
	switch format {
	case FormatXLSX:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case FormatTSV:
		return "text/tab-separated-values; charset=utf-8"
	case FormatXML:
		return "application/xml; charset=utf-8"
	default:
		return "text/csv; charset=utf-8"
	}
}

// Extension is the file extension a written file is given.
func Extension(format Format) string {
	switch format {
	case FormatXLSX:
		return ".xlsx"
	case FormatTSV:
		return ".tsv"
	case FormatXML:
		return ".xml"
	default:
		return ".csv"
	}
}
