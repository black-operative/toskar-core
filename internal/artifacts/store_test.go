package artifacts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/mimir"
	"github.com/yeixio/toskar-core/internal/store"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.SQL.Exec(`INSERT INTO conversations (id, title, created_at, updated_at) VALUES ('c1', 't', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "artifacts")
	return NewStore(db.SQL, dir), dir
}

func TestSaveReadListDelete(t *testing.T) {
	s, dir := newStore(t)
	ctx := context.Background()
	a, err := s.Save(ctx, Input{ConversationID: "c1", Name: "../../etc/report.md", Producer: ProducerUser, Data: []byte("# Hi")})
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "report.md" || a.Kind != "document" || !strings.HasPrefix(a.MimeType, "text/markdown") || a.Size != 4 {
		t.Fatalf("saved %+v", a)
	}
	got, data, err := s.Read(ctx, a.ID)
	if err != nil || string(data) != "# Hi" || got.ConversationID != "c1" {
		t.Fatalf("read %+v %q %v", got, data, err)
	}
	if list, _ := s.List(ctx, "c1"); len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("list %+v", list)
	}
	if err := s.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "c1")); len(entries) != 0 {
		t.Fatal("the file stayed on disk")
	}
}

func TestUploadBeforeTheChatThenAttach(t *testing.T) {
	s, dir := newStore(t)
	ctx := context.Background()
	a, err := s.Save(ctx, Input{Name: "notes.txt", Data: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Attach(ctx, a.ID, "c1"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx, "c1"); len(list) != 1 {
		t.Fatal("attached file is not listed with the chat")
	}
	if err := s.DeleteConversation(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(ctx, "c1"); len(list) != 0 {
		t.Fatal("rows remain")
	}
	if _, err := os.Stat(filepath.Join(dir, "c1")); !os.IsNotExist(err) {
		t.Fatal("folder remains")
	}
	if err := s.DeleteConversation(ctx, "../.."); err != nil {
		t.Fatal(err)
	}
}

func TestTooLarge(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Save(context.Background(), Input{Name: "big.txt", Data: make([]byte, MaxBytes+1)}); err == nil {
		t.Fatal("a file over the limit was saved")
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"report.pdf":        "report.pdf",
		`C:\Users\me\a.txt`: "a.txt",
		"we<i>rd:name?.csv": "we_i_rd_name_.csv",
		"   ":               "file",
		"..":                "file",
		"line\nbreak.md":    "line_break.md",
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSVToXLSXRoundTrips(t *testing.T) {
	raw, err := CSVToXLSX("Prices", "item,price,note\nTire,189.99,\"in stock, 4 left\"\nWiper,12,<cheap> & good\n")
	if err != nil {
		t.Fatal(err)
	}
	passages, err := mimir.FilePassages("prices.xlsx", raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(passages) != 2 {
		t.Fatalf("passages = %+v", passages)
	}
	if passages[0].Body != "item: Tire; price: 189.99; note: in stock, 4 left" || passages[1].Body != "item: Wiper; price: 12; note: <cheap> & good" {
		t.Fatalf("passages = %+v", passages)
	}
	if column(0) != "A" || column(25) != "Z" || column(26) != "AA" || column(701) != "ZZ" {
		t.Fatal("column names")
	}
}
