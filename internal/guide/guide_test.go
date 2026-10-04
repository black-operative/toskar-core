package guide

import (
	"strings"
	"testing"
)

func TestAboutYggdrasil(t *testing.T) {
	cases := map[string]string{
		"How do I schedule an automation?":        "Schedule an automation",
		"Can Yggdrasil train its own AI?":         "Train your own AI",
		"what features do you have?":              "What Yggdrasil does",
		"How do I connect my other computer?":     "Connect your computers",
		"where is what left this computer?":       "Privacy, encryption, and what left this computer",
		"is my data encrypted?":                   "Privacy, encryption, and what left this computer",
		"are my chats private?":                   "Privacy, encryption, and what left this computer",
		"Do I need encryption?":                   "Privacy, encryption, and what left this computer",
		"who can see my chats?":                   "Privacy, encryption, and what left this computer",
		"How do I add knowledge from a folder?":   "Connect knowledge",
		"How do I use an API key with Yggdrasil?": "Connect another app through the API",
		"how to turn on notifications by email?":  "Notifications",
		"what does the Team profile do?":          "Run a Team profile",
		// The product's name, and the one from before the rename (#237).
		"Can Toskar train its own AI?":         "Train your own AI",
		"How do I use an API key with Toskar?": "Connect another app through the API",
	}
	for q, want := range cases {
		ps := About(q)
		if len(ps) == 0 || ps[0].Section != want {
			t.Errorf("About(%q) = %v, want %s first", q, ps, want)
		}
	}
	for _, q := range []string{
		"How do I make pasta carbonara?", "Can you write a poem about the sea?", "What is the capital of France?",
		"How do I fix a flat bike tire?", "Explain quantum computing", "How do I improve my resume?",
		"what is memory in a computer?", "Summarize this: the meeting moved to Tuesday.",
		"How does AES encryption work?", "Is end-to-end encryption safe?",
	} {
		if ps := About(q); len(ps) != 0 {
			t.Errorf("About(%q) = %v, want nothing", q, ps)
		}
	}
}

func TestBlock(t *testing.T) {
	ps := About("How do I schedule an automation?")
	b := Block(ps)
	if !strings.Contains(b, "[Schedule an automation]") || !strings.Contains(b, "say so instead of guessing") {
		t.Fatalf("block = %q", b)
	}
	total := 0
	for _, p := range ps {
		total += len(p.Text)
	}
	if total > maxChars+2000 {
		t.Fatalf("too much guide: %d characters", total)
	}
	if Block(nil) != "" {
		t.Fatal("empty block")
	}
}
