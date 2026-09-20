package reports

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// Report exports. Two formats, one contract: the caller builds headers and
// rows from the query results above, this file turns them into bytes. The
// handlers own the Content-Type and the Content-Disposition filename; these
// writers only ever see an io.Writer, so they are testable without a
// request.
//
// Ported from the retail POS's inventory_reports_export.go (sheetSpec,
// writeWorkbook, neutralizeXLSXCell), with the gin dependency replaced by
// io.Writer and returned errors, and the optional title/money-format columns
// dropped — nothing in §6.8 asks for them.

// WriteCSV writes a header row followed by the data rows, RFC 4180 escaped
// by encoding/csv: a field containing a comma, a double quote or a newline
// is quoted and its quotes doubled.
//
// Deliberately *not* run through NeutralizeXLSXCell. In a workbook only
// strings are neutralised because numbers are written as float64 cells, but
// in a CSV every field is a string — so neutralising here would turn every
// negative amount ("-150.00", a variance or a rounding adjustment) into the
// text "'-150.00" and break the numbers the owner actually opens the file
// for. A caller who needs formula-injection hardening in a CSV must
// neutralise the specific free-text columns itself.
func WriteCSV(w io.Writer, headers []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	if len(headers) > 0 {
		if err := cw.Write(headers); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// sheetSpec is one worksheet: a name, a header row and the data rows. Cell
// values are `any` — write numbers as float64/int so Excel treats them as
// numbers, and strings only for genuine text.
type sheetSpec struct {
	Name    string
	Headers []string
	Rows    [][]any
}

// Sheet is sheetSpec under an exported name, so packages outside this one
// (the reports handlers) can build workbooks. It is an alias, not a new
// type: the two are the same struct, and the package's own tests exercise
// the ported name.
type Sheet = sheetSpec

// neutralizeXLSXCell defends against spreadsheet formula injection: a text
// cell beginning with = + - @ (or a leading tab/CR, which Excel strips
// before parsing) is prefixed with a single quote so Excel renders it
// literally instead of evaluating it. Applied to every cell in
// writeWorkbook, so every user-controlled string in a report — customer and
// product names, void reasons, closing notes — is covered in one place.
// Non-string values pass through unchanged, which is why numeric columns
// must be written as numbers rather than pre-formatted strings.
func neutralizeXLSXCell(v any) any {
	s, ok := v.(string)
	if !ok || s == "" {
		return v
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// NeutralizeXLSXCell is neutralizeXLSXCell under an exported name, for
// callers building cells outside this package.
func NeutralizeXLSXCell(v any) any { return neutralizeXLSXCell(v) }

// writeWorkbook renders the sheets into an .xlsx and streams it to w. The
// first sheet renames excelize's default "Sheet1" rather than adding a
// sheet, so a one-sheet workbook has no stray empty tab.
func writeWorkbook(w io.Writer, sheets []sheetSpec) error {
	if len(sheets) == 0 {
		return fmt.Errorf("reports: workbook needs at least one sheet")
	}
	f := excelize.NewFile()
	defer f.Close()

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"E2E8F0"}, Pattern: 1},
	})
	if err != nil {
		return err
	}

	for i, s := range sheets {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("Sheet%d", i+1)
		}
		if i == 0 {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return err
			}
		} else if _, err := f.NewSheet(name); err != nil {
			return err
		}

		for col, head := range s.Headers {
			cell, err := excelize.CoordinatesToCellName(col+1, 1)
			if err != nil {
				return err
			}
			if err := f.SetCellValue(name, cell, head); err != nil {
				return err
			}
		}
		if n := len(s.Headers); n > 0 {
			first, _ := excelize.CoordinatesToCellName(1, 1)
			last, _ := excelize.CoordinatesToCellName(n, 1)
			if err := f.SetCellStyle(name, first, last, headerStyle); err != nil {
				return err
			}
			if lastCol, err := excelize.ColumnNumberToName(n); err == nil {
				_ = f.SetColWidth(name, "A", lastCol, 18)
			}
			// Freeze the header row so a long report stays readable.
			_ = f.SetPanes(name, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
		}

		for r, row := range s.Rows {
			for col, v := range row {
				cell, err := excelize.CoordinatesToCellName(col+1, r+2)
				if err != nil {
					return err
				}
				if err := f.SetCellValue(name, cell, neutralizeXLSXCell(v)); err != nil {
					return err
				}
			}
		}
	}

	return f.Write(w)
}

// WriteWorkbook is writeWorkbook under an exported name, for the reports
// handlers.
func WriteWorkbook(w io.Writer, sheets []Sheet) error { return writeWorkbook(w, sheets) }
