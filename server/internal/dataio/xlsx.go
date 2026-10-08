package dataio

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// parseXLSX reads one sheet of an Excel workbook.
//
// Cells are read as the text the sheet shows, not as the numbers underneath,
// so a date formatted as a date arrives as a date rather than as Excel's day
// count — and so that turning text into a value stays the import's one job.
func parseXLSX(data []byte, opts Options) (*Table, error) {
	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: not a readable Excel workbook: %v", ErrInvalidSource, err)
	}
	defer file.Close()

	sheets := file.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("%w: the workbook has no sheets", ErrInvalidSource)
	}

	sheet := strings.TrimSpace(opts.Sheet)
	if sheet == "" {
		sheet = sheets[0]
	} else {
		known := false
		for _, s := range sheets {
			if s == sheet {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("%w: the workbook has no sheet named %q (it has %s)",
				ErrInvalidSource, sheet, strings.Join(sheets, ", "))
		}
	}

	raw, err := file.GetRows(sheet)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}

	rows := make([][]string, 0, len(raw))
	truncated := false
	for _, row := range raw {
		// A row of nothing is spacing in a spreadsheet, not a record.
		if isBlankRow(row) {
			continue
		}
		if len(rows) >= opts.Limits.MaxRows {
			truncated = true
			break
		}
		rows = append(rows, row)
	}

	columns, body, err := headerOrNumbered(rows, opts.HasHeader, opts.Limits)
	if err != nil {
		return nil, err
	}
	return &Table{Columns: columns, Rows: body, Sheets: sheets, Truncated: truncated}, nil
}

func isBlankRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
