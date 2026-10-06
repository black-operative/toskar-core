package muninn

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/store"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return NewStore(db.SQL)
}

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in   string
		kind CommandKind
		text string
	}{
		{"Remember that this project uses Go.", CommandRemember, "This project uses Go"},
		{"please remember I prefer metric units", CommandRemember, "I prefer metric units"},
		{"Don't forget: my daughter's name is Ava", CommandRemember, "My daughter's name is Ava"},
		{"Keep in mind that I am vegetarian!", CommandRemember, "I am vegetarian"},
		{"Forget that I prefer metric units", CommandForget, "I prefer metric units"},
		{"forget about the Go project", CommandForget, "The Go project"},
		{"What do you remember about me?", CommandList, ""},
		{"show my memories", CommandList, ""},
		{"My daughter's birthday is May 3, remember that.", CommandRemember, "My daughter's birthday is May 3"},
		{"I'm vegetarian. Please remember", CommandRemember, "I'm vegetarian"},
		{"I live in Juneau — keep that in mind", CommandRemember, "I live in Juneau"},
		{"The wifi password is hunter2, don't forget it!", CommandRemember, "The wifi password is hunter2"},
		{"Can you help me remember", CommandNone, ""},
		{"Ok, remember that.", CommandNone, ""},
		{"Did I tell you, do you remember that?", CommandNone, ""},
		{"Remember this", CommandNone, ""},
		{"Do you remember the capital of France?", CommandNone, ""},
		{"How do I remember things better?", CommandNone, ""},
	}
	for _, c := range cases {
		got := ParseCommand(c.in)
		if got.Kind != c.kind || got.Text != c.text {
			t.Errorf("%q: got %+v, want %s %q", c.in, got, c.kind, c.text)
		}
	}
}

func TestAddDeduplicatesAndRefusesSecrets(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	m, created, err := s.Add(ctx, "I prefer metric units", "", SourceExplicit, "conv-1")
	if err != nil || !created || m.Category != CategoryPreferences {
		t.Fatalf("add = %+v %v %v", m, created, err)
	}
	again, created, err := s.Add(ctx, "i prefer metric units.", "", SourceExplicit, "conv-2")
	if err != nil || created || again.ID != m.ID {
		t.Fatalf("duplicate = %+v %v %v", again, created, err)
	}
	for _, secret := range []string{"My password is hunter2", "my api key is sk-abcdefghijklmnop", "card 4111 1111 1111 1111"} {
		if _, _, err := s.Add(ctx, secret, "", SourceExplicit, ""); !errors.Is(err, ErrSensitive) {
			t.Errorf("%q: err = %v", secret, err)
		}
	}
	// Ordinary uses of words near the credential list are fine.
	if _, _, err := s.Add(ctx, "The surprise party is a secret until Friday", "", SourceManual, ""); err != nil {
		t.Fatalf("harmless memory refused: %v", err)
	}
	all, _ := s.List(ctx)
	if len(all) != 2 {
		t.Fatalf("memories = %+v", all)
	}
}

func TestRelevantIsSmallAndFocused(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	add := func(text string) Memory {
		m, _, err := s.Add(ctx, text, "", SourceManual, "")
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	add("My name is Sam")
	add("I prefer metric units")
	add("This project uses Go and SQLite")
	add("My daughter plays violin")
	off := add("I like jazz")
	disabled := false
	if _, err := s.Update(ctx, off.ID, Patch{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Relevant(ctx, "Which database does the project use?")
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range got {
		texts = append(texts, m.Content)
	}
	joined := strings.Join(texts, "|")
	// Core facts about the person, plus the memory that matches the question.
	if !strings.Contains(joined, "My name is Sam") || !strings.Contains(joined, "metric") || !strings.Contains(joined, "project uses Go") {
		t.Fatalf("relevant = %v", texts)
	}
	if strings.Contains(joined, "violin") || strings.Contains(joined, "jazz") {
		t.Fatalf("irrelevant or disabled memory included: %v", texts)
	}
	if !strings.HasPrefix(Block(got), "What the user asked you to remember.") {
		t.Fatalf("block = %q", Block(got))
	}
	if Block(nil) != "" {
		t.Fatal("no memories, no block")
	}
}

func TestUpdateAndDelete(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	m, _, _ := s.Add(ctx, "I live in Portland", "", SourceManual, "")
	text, cat := "I live in Seattle", CategoryIdentity
	got, err := s.Update(ctx, m.ID, Patch{Content: &text, Category: &cat})
	if err != nil || got.Content != text {
		t.Fatalf("update = %+v %v", got, err)
	}
	if rel, _ := s.Relevant(ctx, "weather in Seattle"); len(rel) != 1 {
		t.Fatalf("updated text not searchable: %+v", rel)
	}
	if err := s.Delete(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, m.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if rel, _ := s.Relevant(ctx, "weather in Seattle"); len(rel) != 0 {
		t.Fatalf("deleted memory still found: %+v", rel)
	}
}

func TestInferCategory(t *testing.T) {
	for text, want := range map[string]string{
		"My name is Sam":                CategoryIdentity,
		"I prefer short answers":        CategoryPreferences,
		"This repo deploys with Docker": CategoryProjects,
		"My wife is a nurse":            CategoryPeople,
		"I have an RTX 4090 GPU":        CategoryTechnical,
		"Tuesday is trash day":          CategoryOther,
	} {
		if got := InferCategory(text); got != want {
			t.Errorf("%q = %s, want %s", text, got, want)
		}
	}
}
