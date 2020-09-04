/*
 * This file is subject to the terms and conditions defined in
 * file 'LICENSE.md', which is part of this source code package.
 */

package extractor

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/unidoc/unipdf/v3/common"
	"github.com/unidoc/unipdf/v3/model"
)

// TestTableCounts checks the number of extracted tables in specified PDFs.
func TestTableCounts(t *testing.T) {
	if len(corpusFolder) == 0 && !forceTest {
		t.Log("Corpus folder not set - skipping")
		return
	}
	for _, g := range tableGoldCounts {
		g.testCounts(t)
	}
}

// tableGoldCounts is our starter set of tables we can currently detect.
var tableGoldCounts = []tableGold{
	{filename: "Speer_Permit.pdf", pageNumTables: map[int]int{1: 1, 2: 2, 3: 1, 4: 0, 5: 0, 6: 1, 7: 2}},
	{filename: "Minerals_to_Metals.pdf", pageNumTables: map[int]int{1: 1, 2: 1, 3: 1, 4: 1, 5: 2, 6: 2, 7: 2}},
	{filename: "Early_Delayed.pdf", pageNumTables: map[int]int{10: 1, 13: 1, 14: 1, 16: 1}},
	{filename: "accounts-payable.pdf", pageNumTables: map[int]int{4: 1, 12: 1}},
	{filename: "results5.pdf", pageNumTables: map[int]int{1: 1}},
	{filename: "Stomlinjer.pdf", pageNumTables: map[int]int{1: 1, 2: 1, 3: 1, 4: 1, 5: 1, 6: 1, 7: 1, 8: 1}},
}

// tableGold describes a PDF file and the number of tables on selected pages.'
// Only the pages specified in pageNumTables are checked.
type tableGold struct {
	filename      string      // Name of PDF.
	pageNumTables map[int]int // {pageNum: num of tables}.
}

// testCounts checks the number of tables in PDF `g.filename` match `g.pageNumTables`.
func (g tableGold) testCounts(t *testing.T) {
	fullpath, exists := corpusFilepath(t, g.filename)
	if !forceTest && !exists {
		return
	}
	numbers := g.pageNums()
	tables, err := extractTables(fullpath, numbers...)
	require.NoError(t, err)
	require.Equalf(t, len(g.pageNumTables), len(tables), "file %q page", fullpath)
	for _, pageNum := range numbers {
		require.Equalf(t, g.pageNumTables[pageNum], len(tables[pageNum]),
			"file %q page %d", fullpath, pageNum)
	}
}

// pageNums returns the (1-offset) page numbers that are to be tested in `g`.
func (g tableGold) pageNums() []int {
	numbers := make([]int, 0, len(g.pageNumTables))
	for pageNum := range g.pageNumTables {
		numbers = append(numbers, pageNum)
	}
	sort.Ints(numbers)
	return numbers
}

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

// stringTable is the strings in TextTable.
// In this testing, all stringTables will be normalized by normalizeTable().
type stringTable [][]string

// extractTables extracts all the tables in the pages given by `pageNumbers` in PDF file `filename`.
// The returned mape is {pageNum: []tables in page pageNum}.
func extractTables(filename string, pageNumbers ...int) (map[int][]stringTable, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("extractTables: Could not open %q err=%v", filename, err)
	}
	defer f.Close()
	pdfReader, err := model.NewPdfReaderLazy(f)
	if err != nil {
		return nil, fmt.Errorf("extractTables: NewPdfReaderLazy failed. %q err=%v", filename, err)
	}

	pageTables := make(map[int][]stringTable)
	if len(pageNumbers) == 0 {
		numPages, err := pdfReader.GetNumPages()
		if err != nil {
			return nil, err
		}
		for pageNum := 1; pageNum < numPages; pageNum++ {
			pageNumbers = append(pageNumbers, pageNum)
		}
	}

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
		tables[i] = asStringTable(table)
	}
	return tables, nil
}

// extractPageTextTables extracts the tables in `page`.
func extractPageTextTables(page *model.PdfPage) ([]TextTable, error) {
	desc, err := NormalizePage(page)
	if err != nil {
		return nil, err
	}
	if desc != "" {
		common.Log.Info("Applied page rotation: %s", desc)
	}

	ex, err := New(page)
	if err != nil {
		return nil, err
	}
	pageText, _, _, err := ex.ExtractPageText()
	if err != nil {
		return nil, err
	}
	return pageText.Tables(), nil
}

func readCsvFile(filename string) (stringTable, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("readCsvFile: Could not open %q err=%v", filename, err)
	}
	defer f.Close()

	csvreader := csv.NewReader(f)
	cells, err := csvreader.ReadAll()
	if err != nil {
		return nil, err
	}
	return normalizeTable(cells), nil
}

// asStringTable returns TextTable `table` as a stringTable.
func asStringTable(table TextTable) stringTable {
	cells := make(stringTable, table.H)
	for y, row := range table.Cells {
		cells[y] = make([]string, table.W)
		for x, cell := range row {
			cells[y][x] = cell.Text
		}
	}
	return normalizeTable(cells)
}

// normalizeTable returns `cells` with each cell normalized.
func normalizeTable(cells stringTable) stringTable {
	for y, row := range cells {
		for x, cell := range row {
			cells[y][x] = normalize(cell)
		}
	}
	return cells
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
