package egress

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/store"
)

func TestLog(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	l := New(db.SQL)
	ctx := WithRun(context.Background(), Run{Source: SourceChat, ConversationID: "c1", TaskID: "t1"})
	l.Add(ctx, WebSearch, "DuckDuckGo", "weather in Juneau")
	l.Add(ctx, WebPage, "wttr.in", "https://wttr.in/juneau")
	l.Add(context.Background(), PairedComputer, "Studio Mac", strings.Repeat("x", 600))
	l.Add(ctx, Connector, "", "no destination is not recorded")

	all, err := l.List(context.Background(), Filter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %+v, %v", all, err)
	}
	if n := len([]rune(all[0].Detail)); all[0].Kind != PairedComputer || n != maxDetail+1 {
		t.Fatalf("newest = %+v (%d runes)", all[0], n)
	}
	chat, _ := l.List(context.Background(), Filter{ConversationID: "c1"})
	if len(chat) != 2 || chat[1].Kind != WebSearch || chat[1].Detail != "weather in Juneau" || chat[1].Source != SourceChat || chat[1].TaskID != "t1" {
		t.Fatalf("chat = %+v", chat)
	}
	sum, _ := l.Summary(context.Background(), time.Now().Add(-time.Hour))
	if sum[WebSearch] != 1 || sum[WebPage] != 1 || sum[PairedComputer] != 1 {
		t.Fatalf("summary = %+v", sum)
	}
	var nilLog *Log
	nilLog.Add(ctx, WebSearch, "x", "y") // a missing log never fails the work
}
