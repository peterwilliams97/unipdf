/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"encoding/csv"
	"fmt"
	"os"
	"testing"

	"github.com/unidoc/unipdf/v3/contentstream"
	"github.com/unidoc/unipdf/v3/core"
	"github.com/unidoc/unipdf/v3/model"
)

// stringTable is the strings in TextTable.
type stringTable [][]string

// TestTextExtractionFragments tests text extraction on the PDF fragments in `fragmentTests`.
func TestTableReference(t *testing.T) {
	if len(corpusFolder) == 0 && !forceTest {
		t.Log("Corpus folder not set - skipping")
		return
	}
	for _, er := range tableReferenceTests {
		er.runTableTest(t)
	}
}

// tableReferenceTests compare tables extracted from a page of a PDF file to a reference text file.
var tableReferenceTests = []extractReference{
	{"COVID-19.pdf", 4},
}

// compareExtractedTablesToReference extracts tables from (1-offset) page `pageNum` of PDF `filename`
// and checks that those tables contain all the tables in the CSV files in `csvPaths`.
func compareExtractedTablesToReference(t *testing.T, filename string, pageNum int, csvPaths []string) {
	expectedTables := make([]stringTable, len(csvPaths))
	for i, path := range csvPaths {
		table, err := readCsvFile(path)
		if err != nil {
			t.Fatalf("readCsvFile failed. Path=%q err=%v", path, err)
		}
		expectedTables[i] = table
	}

	actualTables, err := extractTables(filename, pageNum)
	if err != nil {
		t.Fatalf("extractTables failed. filename=%q pageNum=%d err=%v", filename, pageNum, err)
	}

	for _, aTable := range actualTables[pageNum] {
		found := false
		for _, eTable := range expectedTables {
			if containsTable(aTable, eTable) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("Table mismatch filename=%q page=%d", filename, pageNum)
		}
	}
}

// extractTables extracts all the tables in the pages given by `pageNumbers` in PDF file `filename`.
// The returned mape is {pageNum: []tables in page pageNum}.
func extractTables(filename string, pageNumbers ...int) (map[int][]stringTable, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("Could not open %q err=%v", filename, err)
	}
	defer f.Close()
	pdfReader, err := model.NewPdfReaderLazy(f)
	if err != nil {
		return nil, fmt.Errorf("NewPdfReaderLazy failed. %q err=%v", filename, err)
	}

	pageTables := map[int][]stringTable{}

	for _, pageNum := range pageNumbers {
		page, err := pdfReader.GetPage(pageNum)
		if err != nil {
			return nil, fmt.Errorf("extractTables: GetPage failed. filename=%q err=%v", filename, err)
		}
		tables, err := extractPageTables(page)
		if err != nil {
			return nil, fmt.Errorf("extractTables: extractPageTables failed. filename=%q err=%v",
				filename, err)
		}
		pageTables[pageNum] = tables
	}
	return pageTables, nil
}

// extractPageTables extracts the tables in `page` and returns them as stringTables.
func extractPageTables(page *model.PdfPage) ([]stringTable, error) {
	textTables, err := extractPageTextTables(page)
	if err != nil {
		return nil, err
	}
	tables := make([]stringTable, len(textTables))
	for i, table := range textTables {
		tables[i] = asStrings(table)
	}
	return tables, nil
}

// extractPageTextTables extracts the tables in `page`.
func extractPageTextTables(page *model.PdfPage) ([]TextTable, error) {
	mbox, err := page.GetMediaBox()
	if err != nil {
		return nil, err
	}
	fmt.Printf("%.0f ", *mbox)
	if page.Rotate != nil && *page.Rotate == 90 {
		// TODO: This is a "hack" to change the perspective of the extractor to account for the rotation.
		contents, err := page.GetContentStreams()
		if err != nil {
			return nil, err
		}

		cc := contentstream.NewContentCreator()
		cc.Translate(mbox.Width()/2, mbox.Height()/2)
		cc.RotateDeg(-90)
		cc.Translate(-mbox.Width()/2, -mbox.Height()/2)
		rotateOps := cc.Operations().String()
		contents = append([]string{rotateOps}, contents...)

		page.Duplicate()
		if err = page.SetContentStreams(contents, core.NewRawEncoder()); err != nil {
			return nil, fmt.Errorf("SetContentStreams failed. err=%v", err)
		}
		page.Rotate = nil
	}

	ex, err := New(page)
	if err != nil {
		return nil, fmt.Errorf("extractor.New failed err=%v", err)
	}
	pageText, _, _, err := ex.ExtractPageText()
	if err != nil {
		return nil, fmt.Errorf("ExtractPageText failed. err=%v", err)
	}
	return pageText.Tables(), nil
}

// containsTable returns true if `aTable` contains `eTable`.
func containsTable(aTable, eTable stringTable) bool {
	aH, aW := len(aTable), len(aTable[0])
	eH, eW := len(eTable), len(eTable[0])
	if aH < eH || aW < eW {
		return false
	}
	x0, y0 := -1, -1
	for y := 0; y < aH; y++ {
		for x := 0; x < aW; x++ {
			if aTable[y][x] == eTable[0][0] {
				x0, y0 = x, y
				break
			}
		}
	}
	if x0 < 0 {
		return false
	}

	for y := 0; y < eH; y++ {
		for x := 0; x < eW; x++ {
			if aTable[y+y0][x+x0] != eTable[y][x] {
				return false
			}
		}
	}
	return true
}

func readCsvFile(filename string) (stringTable, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("Could not open %q err=%v", filename, err)
	}
	defer f.Close()

	csvreader := csv.NewReader(f)
	cells, err := csvreader.ReadAll()
	if err != nil {
		return nil, err
	}
	return normalizeTable(cells), nil
}

func asStrings(table TextTable) stringTable {
	cells := make(stringTable, table.H)
	for y, row := range table.Cells {
		cells[y] = make([]string, table.W)
		for x, cell := range row {
			cells[y][x] = cell.Text
		}
	}
	return normalizeTable(cells)
}

func normalizeTable(cells stringTable) stringTable {
	for y, row := range cells {
		for x, cell := range row {
			cells[y][x] = normalize(cell)
		}
	}
	return cells
}
