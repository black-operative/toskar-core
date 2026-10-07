package app

import "testing"

// A run's sources are what its read-only tools returned: the same reads
// give the same fingerprint, a different page a different one, and a tool
// that changes things isn't counted (#204).
func TestSourceRecorder(t *testing.T) {
	read := func(page string) string {
		var s sourceRecorder
		s.add("internet.search", map[string]any{"query": "jobs"}, map[string]any{"results": []any{"a", "b"}})
		s.add("internet.open", map[string]any{"url": "https://example.com/jobs"}, map[string]any{"content": page})
		return s.sum()
	}
	first, again := read("3 openings"), read("3 openings")
	if first != again {
		t.Fatal("the same reads gave different fingerprints")
	}
	if read("3 openings") == read("4 openings") {
		t.Fatal("a changed page kept its fingerprint")
	}
	var none sourceRecorder
	if none.sum() != "" {
		t.Fatal("a run that read nothing has a fingerprint")
	}
	var writes sourceRecorder
	writes.add("files.create", map[string]any{"name": "x.md"}, map[string]any{"id": "1"})
	if writes.sum() != "" {
		t.Fatal("a tool that creates files was counted as a source")
	}
}
