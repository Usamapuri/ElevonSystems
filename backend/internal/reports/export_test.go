package reports

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// The formula-injection guard, cell by cell. Every character Excel treats as
// the start of a formula — and the two whitespace characters it strips
// before deciding — must come back quoted.
func TestNeutralizeXLSXCell(t *testing.T) {
	cases := []struct {
		in   any
		want any
	}{
		{"=SUM(A1)", "'=SUM(A1)"},
		{"=cmd|' /C calc'!A0", "'=cmd|' /C calc'!A0"},
		{"+1+1", "'+1+1"},
		{"-1-1", "'-1-1"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\t=SUM(A1)", "'\t=SUM(A1)"},
		{"\r=SUM(A1)", "'\r=SUM(A1)"},
		// Harmless text is untouched, and so is anything that is not a
		// string — which is why money must be written as a number.
		{"Gas Traders", "Gas Traders"},
		{"", ""},
		{"a=b", "a=b"},
		{-50.0, -50.0},
		{1180, 1180},
		{nil, nil},
	}
	for _, c := range cases {
		if got := neutralizeXLSXCell(c.in); got != c.want {
			t.Errorf("neutralizeXLSXCell(%#v) = %#v, want %#v", c.in, got, c.want)
		}
		if got := NeutralizeXLSXCell(c.in); got != c.want {
			t.Errorf("NeutralizeXLSXCell(%#v) = %#v, want %#v", c.in, got, c.want)
		}
	}
}

// The guard has to survive the round trip into the file: a neutralised cell
// must come back as text, and must not be a formula.
func TestWriteWorkbookNeutralisesAndKeepsNumbersNumeric(t *testing.T) {
	var buf bytes.Buffer
	err := writeWorkbook(&buf, []sheetSpec{
		{
			Name:    "Daily",
			Headers: []string{"Date", "Customer", "Net"},
			Rows: [][]any{
				{"10-03-2026", "=SUM(A1)", 1770.0},
				{"11-03-2026", "Gas Traders", -50.5},
			},
		},
		{
			Name:    "Tax",
			Headers: []string{"Rate", "Tax"},
			Rows:    [][]any{{0.18, 702.0}},
		},
	})
	if err != nil {
		t.Fatalf("writeWorkbook: %v", err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("reopen workbook: %v", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) != 2 || sheets[0] != "Daily" || sheets[1] != "Tax" {
		t.Fatalf("sheets = %v, want [Daily Tax]", sheets)
	}

	// Headers on row 1, data from row 2.
	if got, _ := f.GetCellValue("Daily", "A1"); got != "Date" {
		t.Errorf("A1 = %q, want Date", got)
	}
	if got, _ := f.GetCellValue("Daily", "A2"); got != "10-03-2026" {
		t.Errorf("A2 = %q", got)
	}

	if got, _ := f.GetCellValue("Daily", "B2"); got != "'=SUM(A1)" {
		t.Errorf("B2 = %q, want the quoted literal '=SUM(A1)", got)
	}
	if formula, _ := f.GetCellFormula("Daily", "B2"); formula != "" {
		t.Errorf("B2 became a formula: %q", formula)
	}
	// A name with no leading trigger character is left alone.
	if got, _ := f.GetCellValue("Daily", "B3"); got != "Gas Traders" {
		t.Errorf("B3 = %q", got)
	}

	// Numbers stay numbers — a negative amount is not mangled into text.
	if got, _ := f.GetCellValue("Daily", "C2"); got != "1770" {
		t.Errorf("C2 = %q, want 1770", got)
	}
	if got, _ := f.GetCellValue("Daily", "C3"); got != "-50.5" {
		t.Errorf("C3 = %q, want -50.5", got)
	}
	if typ, err := f.GetCellType("Daily", "C3"); err != nil || typ == excelize.CellTypeSharedString || typ == excelize.CellTypeInlineString {
		t.Errorf("C3 cell type = %v (err %v), want a numeric cell", typ, err)
	}

	if got, _ := f.GetCellValue("Tax", "B2"); got != "702" {
		t.Errorf("Tax!B2 = %q", got)
	}
}

func TestWriteWorkbookRejectsEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := writeWorkbook(&buf, nil); err == nil {
		t.Fatal("an empty workbook must be an error, not a zero-sheet file")
	}
}

// Sheet is the exported alias handlers build with; it must be the same type
// the ported writer takes.
func TestSheetAliasIsUsable(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteWorkbook(&buf, []Sheet{{Name: "S", Headers: []string{"H"}, Rows: [][]any{{"v"}}}}); err != nil {
		t.Fatalf("WriteWorkbook: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("no bytes written")
	}
}

// RFC 4180 escaping: commas, quotes and newlines are quoted, everything else
// is written as given. In particular a negative amount stays "-50.00" — the
// formula guard deliberately does not run over CSV fields, because in a CSV
// every field is a string and quoting them all would break the numbers.
func TestWriteCSVEscaping(t *testing.T) {
	var buf bytes.Buffer
	err := WriteCSV(&buf,
		[]string{"Customer", "Note", "Amount"},
		[][]string{
			{"Gas Traders, Ltd", `He said "hi"`, "1180.00"},
			{"Line\nBreak", "plain", "-50.00"},
			{"", "", "0"},
		})
	if err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}

	want := strings.Join([]string{
		`Customer,Note,Amount`,
		`"Gas Traders, Ltd","He said ""hi""",1180.00`,
		`"Line`,
		`Break",plain,-50.00`,
		`,,0`,
		``,
	}, "\n")
	if got := buf.String(); got != want {
		t.Errorf("CSV =\n%q\nwant\n%q", got, want)
	}
}

func TestWriteCSVHeadersOptional(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCSV(&buf, nil, [][]string{{"a", "b"}}); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	if got := buf.String(); got != "a,b\n" {
		t.Errorf("CSV = %q", got)
	}
}
