package dataio_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/file4base/file4base-app/server/internal/dataio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestFormatFromName(t *testing.T) {
	for name, want := range map[string]dataio.Format{
		"customers.csv": dataio.FormatCSV,
		"CUSTOMERS.CSV": dataio.FormatCSV,
		"customers.tsv": dataio.FormatTSV,
		"customers.tab": dataio.FormatTSV,
		"customers.txt": dataio.FormatTSV,
		"book.xlsx":     dataio.FormatXLSX,
		"book.xlsm":     dataio.FormatXLSX,
		"export.xml":    dataio.FormatXML,
		"no extension":  dataio.FormatCSV,
	} {
		assert.Equal(t, want, dataio.FormatFromName(name), name)
	}
}

func TestParseDelimited(t *testing.T) {
	t.Run("a header row names the columns", func(t *testing.T) {
		table, err := dataio.Parse([]byte("Last Name,City\nDurand,Paris\nSmith,New York\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true})
		require.NoError(t, err)

		assert.Equal(t, []string{"Last Name", "City"}, table.Columns)
		assert.Equal(t, [][]string{{"Durand", "Paris"}, {"Smith", "New York"}}, table.Rows)
		assert.False(t, table.Truncated)
	})

	t.Run("without one the columns are numbered", func(t *testing.T) {
		table, err := dataio.Parse([]byte("Durand,Paris\n"),
			dataio.Options{Format: dataio.FormatCSV})
		require.NoError(t, err)
		assert.Equal(t, []string{"Column 1", "Column 2"}, table.Columns)
		assert.Len(t, table.Rows, 1)
	})

	t.Run("a blank header cell still names its column", func(t *testing.T) {
		table, err := dataio.Parse([]byte("Last Name,,City\na,b,c\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true})
		require.NoError(t, err)
		assert.Equal(t, []string{"Last Name", "Column 2", "City"}, table.Columns,
			"the columns after it must not shift up by one")
	})

	t.Run("the separator is worked out when it is not given", func(t *testing.T) {
		tab, err := dataio.Parse([]byte("a\tb\tc\n1\t2\t3\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, tab.Columns)

		semi, err := dataio.Parse([]byte("a;b;c\n1;2;3\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, semi.Columns,
			"a European export is semicolon-separated")
	})

	t.Run("a comma inside a value does not make it the separator", func(t *testing.T) {
		// Tabs separate; the commas are part of the addresses.
		table, err := dataio.Parse([]byte("name\taddress\nDurand\t14 Avenue Foch, Paris\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true})
		require.NoError(t, err)
		assert.Equal(t, []string{"name", "address"}, table.Columns)
		assert.Equal(t, "14 Avenue Foch, Paris", table.Rows[0][1])
	})

	t.Run("a quoted value keeps its commas and newlines", func(t *testing.T) {
		table, err := dataio.Parse([]byte("name,address\nDurand,\"14 Avenue Foch,\nParis\"\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true, Delimiter: ","})
		require.NoError(t, err)
		assert.Equal(t, "14 Avenue Foch,\nParis", table.Rows[0][1])
	})

	t.Run("a byte order mark does not become part of the first column", func(t *testing.T) {
		data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("Last Name,City\nDurand,Paris\n")...)
		table, err := dataio.Parse(data, dataio.Options{Format: dataio.FormatCSV, HasHeader: true})
		require.NoError(t, err)
		assert.Equal(t, "Last Name", table.Columns[0])
	})

	t.Run("short rows are rows with empty cells, not a broken file", func(t *testing.T) {
		table, err := dataio.Parse([]byte("a,b,c\n1,2,3\n4,5\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true, Delimiter: ","})
		require.NoError(t, err)
		require.Len(t, table.Rows, 2)
		assert.Equal(t, "", table.Cell(1, 2), "a cell past the end of a short row is empty")
	})

	t.Run("blank lines are skipped", func(t *testing.T) {
		table, err := dataio.Parse([]byte("a,b\n1,2\n\n\n3,4\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true, Delimiter: ","})
		require.NoError(t, err)
		assert.Len(t, table.Rows, 2)
	})

	t.Run("an empty file reads as nothing rather than failing", func(t *testing.T) {
		table, err := dataio.Parse([]byte(""), dataio.Options{Format: dataio.FormatCSV, HasHeader: true})
		require.NoError(t, err)
		assert.Empty(t, table.Rows)
	})
}

func TestParseLimits(t *testing.T) {
	t.Run("a file past the byte limit is refused before it is read", func(t *testing.T) {
		_, err := dataio.Parse(bytes.Repeat([]byte("a,b\n"), 100),
			dataio.Options{Format: dataio.FormatCSV, Limits: dataio.Limits{MaxBytes: 10}})
		assert.ErrorIs(t, err, dataio.ErrSourceTooLarge)
	})

	t.Run("rows past the limit are left out and said so", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("a,b\n")
		for i := 0; i < 50; i++ {
			fmt.Fprintf(&b, "%d,%d\n", i, i)
		}
		table, err := dataio.Parse([]byte(b.String()),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true, Limits: dataio.Limits{MaxRows: 10}})
		require.NoError(t, err)
		assert.Len(t, table.Rows, 9, "ten rows read, one of them the header")
		assert.True(t, table.Truncated, "the caller must be able to say the file was cut")
	})

	t.Run("too many columns is refused", func(t *testing.T) {
		header := make([]string, 20)
		for i := range header {
			header[i] = fmt.Sprintf("c%d", i)
		}
		_, err := dataio.Parse([]byte(strings.Join(header, ",")+"\n"),
			dataio.Options{Format: dataio.FormatCSV, HasHeader: true, Limits: dataio.Limits{MaxColumns: 5}})
		assert.ErrorIs(t, err, dataio.ErrSourceTooLarge)
	})
}

func writeWorkbook(t *testing.T, sheets map[string][][]string, order []string) []byte {
	t.Helper()
	f := excelize.NewFile()
	for i, name := range order {
		if i == 0 {
			require.NoError(t, f.SetSheetName("Sheet1", name))
		} else {
			_, err := f.NewSheet(name)
			require.NoError(t, err)
		}
		for r, row := range sheets[name] {
			for c, cell := range row {
				axis, err := excelize.CoordinatesToCellName(c+1, r+1)
				require.NoError(t, err)
				require.NoError(t, f.SetCellStr(name, axis, cell))
			}
		}
	}
	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))
	return buf.Bytes()
}

func TestParseXLSX(t *testing.T) {
	data := writeWorkbook(t, map[string][][]string{
		"Customers": {{"Last Name", "City"}, {"Durand", "Paris"}, {"Smith", "New York"}},
		"Notes":     {{"note"}, {"nothing"}},
	}, []string{"Customers", "Notes"})

	t.Run("the first sheet is read, and the others are offered", func(t *testing.T) {
		table, err := dataio.Parse(data, dataio.Options{Format: dataio.FormatXLSX, HasHeader: true})
		require.NoError(t, err)
		assert.Equal(t, []string{"Last Name", "City"}, table.Columns)
		assert.Equal(t, [][]string{{"Durand", "Paris"}, {"Smith", "New York"}}, table.Rows)
		assert.Equal(t, []string{"Customers", "Notes"}, table.Sheets)
	})

	t.Run("another sheet can be asked for by name", func(t *testing.T) {
		table, err := dataio.Parse(data,
			dataio.Options{Format: dataio.FormatXLSX, HasHeader: true, Sheet: "Notes"})
		require.NoError(t, err)
		assert.Equal(t, []string{"note"}, table.Columns)
	})

	t.Run("a sheet that is not there is reported, with the ones that are", func(t *testing.T) {
		_, err := dataio.Parse(data,
			dataio.Options{Format: dataio.FormatXLSX, Sheet: "No Such Sheet"})
		require.ErrorIs(t, err, dataio.ErrInvalidSource)
		assert.Contains(t, err.Error(), "Customers")
	})

	t.Run("something that is not a workbook is refused", func(t *testing.T) {
		_, err := dataio.Parse([]byte("Last Name,City\n"), dataio.Options{Format: dataio.FormatXLSX})
		assert.ErrorIs(t, err, dataio.ErrInvalidSource)
	})
}

func TestParseXML(t *testing.T) {
	t.Run("the repeating element is worked out", func(t *testing.T) {
		table, err := dataio.Parse([]byte(`
			<customers>
			  <customer id="1"><last_name>Durand</last_name><city>Paris</city></customer>
			  <customer id="2"><last_name>Smith</last_name><city>New York</city></customer>
			</customers>`), dataio.Options{Format: dataio.FormatXML})
		require.NoError(t, err)

		assert.Equal(t, []string{"@id", "last_name", "city"}, table.Columns,
			"an attribute is a field too, so an id is not lost")
		assert.Equal(t, [][]string{
			{"1", "Durand", "Paris"},
			{"2", "Smith", "New York"},
		}, table.Rows)
	})

	t.Run("a record missing a field has an empty cell", func(t *testing.T) {
		table, err := dataio.Parse([]byte(`
			<rows>
			  <row><a>1</a><b>2</b></row>
			  <row><a>3</a></row>
			</rows>`), dataio.Options{Format: dataio.FormatXML})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, table.Columns)
		assert.Equal(t, "", table.Cell(1, 1))
	})

	t.Run("the record element can be named", func(t *testing.T) {
		table, err := dataio.Parse([]byte(`
			<data>
			  <wrapper><item><a>1</a></item><item><a>2</a></item></wrapper>
			</data>`), dataio.Options{Format: dataio.FormatXML, RecordElement: "item"})
		require.NoError(t, err)
		assert.Len(t, table.Rows, 2)
	})

	t.Run("FileMaker's own grammar is read", func(t *testing.T) {
		table, err := dataio.Parse([]byte(`<?xml version="1.0"?>
			<FMPXMLRESULT xmlns="http://www.filemaker.com/fmpxmlresult">
			  <METADATA>
			    <FIELD NAME="Last Name" TYPE="TEXT"/>
			    <FIELD NAME="City" TYPE="TEXT"/>
			  </METADATA>
			  <RESULTSET FOUND="2">
			    <ROW><COL><DATA>Durand</DATA></COL><COL><DATA>Paris</DATA></COL></ROW>
			    <ROW><COL><DATA>Smith</DATA></COL><COL><DATA>New York</DATA></COL></ROW>
			  </RESULTSET>
			</FMPXMLRESULT>`), dataio.Options{Format: dataio.FormatXML})
		require.NoError(t, err)

		assert.Equal(t, []string{"Last Name", "City"}, table.Columns)
		assert.Equal(t, [][]string{{"Durand", "Paris"}, {"Smith", "New York"}}, table.Rows)
	})

	t.Run("a repeating field becomes one value per line", func(t *testing.T) {
		table, err := dataio.Parse([]byte(`
			<FMPXMLRESULT>
			  <METADATA><FIELD NAME="Types"/></METADATA>
			  <RESULTSET><ROW><COL><DATA>New</DATA><DATA>Continuing</DATA></COL></ROW></RESULTSET>
			</FMPXMLRESULT>`), dataio.Options{Format: dataio.FormatXML})
		require.NoError(t, err)
		assert.Equal(t, "New\nContinuing", table.Rows[0][0],
			"which is how File4Base holds several values in one field")
	})

	t.Run("a file holding one record reads as one record", func(t *testing.T) {
		// Nothing repeats, but <a> is still a record with a field in it.
		// Reading it is more use than refusing it.
		table, err := dataio.Parse([]byte(`<a><b>1</b></a>`), dataio.Options{Format: dataio.FormatXML})
		require.NoError(t, err)
		assert.Equal(t, []string{"b"}, table.Columns)
		assert.Equal(t, [][]string{{"1"}}, table.Rows)
	})

	t.Run("a file with no elements at all is reported", func(t *testing.T) {
		_, err := dataio.Parse([]byte("   "), dataio.Options{Format: dataio.FormatXML})
		assert.ErrorIs(t, err, dataio.ErrInvalidSource)
	})

	t.Run("something that is not XML is refused", func(t *testing.T) {
		_, err := dataio.Parse([]byte("Last Name,City\nDurand,Paris"),
			dataio.Options{Format: dataio.FormatXML})
		assert.ErrorIs(t, err, dataio.ErrInvalidSource)
	})
}

func TestSample(t *testing.T) {
	table := &dataio.Table{Rows: [][]string{{"1"}, {"2"}, {"3"}}}
	assert.Len(t, table.Sample(2), 2)
	assert.Len(t, table.Sample(10), 3)
	assert.Len(t, table.Sample(0), 3)
}
