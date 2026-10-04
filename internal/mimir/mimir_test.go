package mimir

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db.SQL, filepath.Join(t.TempDir(), "knowledge"))
}

const inventory = `sku,brand,model,size,price,in_stock
MP-22545,Michelin,Pilot Sport 4,225/45R17,189.99,12
BW-20555,Bridgestone,Blizzak WS90,205/55R16,142.50,0
`

func TestTableRowsAreSearchableBySize(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "inventory.csv", Text: inventory})
	if err != nil {
		t.Fatal(err)
	}
	if src.Status != StatusReady || src.ChunkCount != 2 {
		t.Fatalf("got status %q with %d chunks: %s", src.Status, src.ChunkCount, src.Error)
	}
	hits, err := s.Search(ctx, SearchInput{Query: "Do you have 225/45R17 tires?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Body, "sku: MP-22545") {
		t.Fatalf("want the Michelin row first, got %+v", hits)
	}
	if !strings.Contains(hits[0].Body, "price: 189.99") {
		t.Fatalf("row passage lost its columns: %q", hits[0].Body)
	}
}

func TestPathSourceRefreshesWhenFileChanges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "prices.csv")
	if err := os.WriteFile(file, []byte(inventory), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := s.Create(ctx, CreateInput{Kind: KindPath, Path: file})
	if err != nil {
		t.Fatal(err)
	}

	// The business changes a price. No retraining, no manual refresh.
	updated := strings.Replace(inventory, "189.99", "174.00", 1)
	if err := os.WriteFile(file, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}

	hits, err := s.Search(ctx, SearchInput{Query: "MP-22545 price", SourceIDs: []string{src.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Body, "174.00") {
		t.Fatalf("search did not see the new price: %+v", hits)
	}
}

func TestFolderSourceSkipsUnsupportedAndHiddenFiles(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	dir := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "returns.md"), []byte("# Returns\n\nTires can be returned within 30 days if unmounted."), 0o600))
	must(os.WriteFile(filepath.Join(dir, "photo.png"), []byte{0x89, 0x50}, 0o600))
	must(os.MkdirAll(filepath.Join(dir, ".git"), 0o755))
	must(os.WriteFile(filepath.Join(dir, ".git", "notes.txt"), []byte("secret"), 0o600))

	src, err := s.Create(ctx, CreateInput{Kind: KindPath, Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	if src.ChunkCount != 1 {
		t.Fatalf("want 1 chunk from returns.md, got %d", src.ChunkCount)
	}
	hits, err := s.Search(ctx, SearchInput{Query: "return policy days"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Title, "Returns") {
		t.Fatalf("got %+v", hits)
	}
}

func TestSearchOnlyUsesRequestedSources(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "a.txt", Text: "Alpha warranty lasts five years."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "b.txt", Text: "Beta warranty lasts one year."}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, SearchInput{Query: "warranty", SourceIDs: []string{a.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].SourceID != a.ID {
		t.Fatalf("got %+v", hits)
	}
}

func TestUpdateAndDeleteTextSource(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	src, err := s.Create(ctx, CreateInput{Kind: KindText, Text: "Store hours are 9 to 5."})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Content(ctx, src.ID); err != nil || got != "Store hours are 9 to 5." {
		t.Fatalf("content = %q %v", got, err)
	}
	text := "Store hours are 8 to 6."
	if _, err := s.Update(ctx, src.ID, UpdateInput{Text: &text}); err != nil {
		t.Fatal(err)
	}
	hits, _ := s.Search(ctx, SearchInput{Query: "store hours"})
	if len(hits) != 1 || !strings.Contains(hits[0].Body, "8 to 6") {
		t.Fatalf("got %+v", hits)
	}
	if err := s.Delete(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	hits, _ = s.Search(ctx, SearchInput{Query: "store hours"})
	if len(hits) != 0 {
		t.Fatalf("deleted source still searchable: %+v", hits)
	}
	if _, err := s.Get(ctx, src.ID); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestMissingPathIsRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create(context.Background(), CreateInput{Kind: KindPath, Path: filepath.Join(t.TempDir(), "nope")}); err == nil {
		t.Fatal("want an error for a missing path")
	}
}

func TestMatchQueryQuotesTermsAndDropsStopwords(t *testing.T) {
	got := matchQuery(`What is the price of "AB-1002" in 225/45R17?`)
	want := `"price" OR "ab-1002" OR "225/45r17"`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestChunkTextCarriesHeadings(t *testing.T) {
	text := "# Fitment\n\nAsk for year, make, and model.\n\n## Winter\n\n" + strings.Repeat("Snow tires grip better. ", 80)
	chunks := chunkText("guide.md", text)
	if len(chunks) < 2 {
		t.Fatalf("want the long section split, got %d chunks", len(chunks))
	}
	if !strings.Contains(chunks[len(chunks)-1].Title, "Winter") {
		t.Fatalf("last chunk lost its heading: %q", chunks[len(chunks)-1].Title)
	}
}

func TestContextBlockRespectsBudget(t *testing.T) {
	hits := []Hit{{Title: "a", Body: strings.Repeat("x", 500)}, {Title: "b", Body: strings.Repeat("y", 500)}}
	block := ContextBlock(hits, 700)
	if !strings.Contains(block, "[1] a") || strings.Contains(block, "[2] b") {
		t.Fatalf("budget not applied: %q", block)
	}
	if ContextBlock(nil, 0) != "" {
		t.Fatal("empty hits should add nothing")
	}
}

func TestSearchDropsWeakMatches(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	text := "sku,brand,model,size,price\n" +
		"MP-22545,Michelin,Pilot Sport 4,225/45R17,189.99\n" +
		"MD-21555,Michelin,Defender2,215/55R17,176.50\n" +
		"MX-23565,Michelin,CrossClimate2,235/65R17,211.00\n" +
		"BW-20555,Bridgestone,Blizzak WS90,205/55R16,142.50\n"
	if _, err := s.Create(ctx, CreateInput{Kind: KindText, Filename: "stock.csv", Text: text}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.Search(ctx, SearchInput{Query: "How much is the Michelin Pilot Sport 4 in 225/45R17?"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Body, "MP-22545") {
		t.Fatalf("want only the Pilot Sport 4 row, got %d: %+v", len(hits), hits)
	}
	// A broad question still returns every match.
	broad, _ := s.Search(ctx, SearchInput{Query: "Michelin"})
	if len(broad) != 3 {
		t.Fatalf("broad query = %d hits", len(broad))
	}
}
