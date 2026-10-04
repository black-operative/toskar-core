package benchmark

import "github.com/yeixio/toskar-core/pkg/contracts"

// Workloads returns the built-in workload catalog.
func Workloads() []contracts.BenchmarkWorkload {
	return []contracts.BenchmarkWorkload{
		{
			ID:          "coding",
			Name:        "Coding",
			Description: "Function writing, debugging, and short refactors.",
			Prompts: []contracts.BenchmarkPrompt{
				{
					ID:    "coding-fn",
					Label: "Write a function",
					Text:  `Write a Python function that takes a list of integers and returns the longest contiguous subarray with a sum of zero. Include a brief docstring and one example.`,
				},
				{
					ID:    "coding-bug",
					Label: "Find a bug",
					Text:  "This Go function is supposed to reverse a string in place using runes, but it has a bug. Explain the bug and show a corrected version:\n\nfunc reverse(s string) string {\n  r := []rune(s)\n  for i := 0; i < len(r); i++ {\n    r[i], r[len(r)-1-i] = r[len(r)-1-i], r[i]\n  }\n  return string(r)\n}",
				},
				{
					ID:    "coding-refactor",
					Label: "Refactor",
					Text:  "Refactor this JavaScript into clearer, typed TypeScript with early returns and no nested ternaries:\n\nfunction score(u){return u?u.active?u.tier==='pro'?100:u.tier==='plus'?50:10:0:0}",
				},
			},
		},
		{
			ID:          "writing",
			Name:        "Writing",
			Description: "Prose quality, tone control, and editing.",
			Prompts: []contracts.BenchmarkPrompt{
				{
					ID:    "writing-essay",
					Label: "Short essay",
					Text:  "Write three tight paragraphs about why local-first AI matters for privacy-conscious teams. Professional tone, no hype.",
				},
				{
					ID:    "writing-rewrite",
					Label: "Rewrite tone",
					Text:  "Rewrite this email to sound warmer and clearer without losing the ask:\n\nHi — Need the Q3 report by Friday. Also fix the dashboard bugs. Thanks.",
				},
				{
					ID:    "writing-outline",
					Label: "Outline",
					Text:  "Create a concise outline for a blog post titled “Choosing a small local LLM for a laptop.” Five sections max.",
				},
			},
		},
		{
			ID:          "chat",
			Name:        "Chat",
			Description: "Everyday Q&A and helpful conversational replies.",
			Prompts: []contracts.BenchmarkPrompt{
				{
					ID:    "chat-howto",
					Label: "How-to",
					Text:  "I just installed a local AI app on my Mac. In plain language, how do I pick a model that won’t freeze my machine?",
				},
				{
					ID:    "chat-compare",
					Label: "Compare options",
					Text:  "Compare headphones vs speakers for focused coding sessions. Give three practical tradeoffs.",
				},
				{
					ID:    "chat-troubleshoot",
					Label: "Troubleshoot",
					Text:  "My local model answers slowly after the first message, then gets faster. What are the most likely causes?",
				},
			},
		},
		{
			ID:          "reasoning",
			Name:        "Reasoning",
			Description: "Multi-step logic and careful deduction.",
			Prompts: []contracts.BenchmarkPrompt{
				{
					ID:    "reason-puzzle",
					Label: "Logic puzzle",
					Text:  "A farmer has to cross a river with a wolf, a goat, and a cabbage. The boat holds the farmer plus one item. The wolf cannot be left alone with the goat, and the goat cannot be left alone with the cabbage. List the minimal sequence of crossings.",
				},
				{
					ID:    "reason-math",
					Label: "Word problem",
					Text:  "A store sells laptops at $900. During a sale they discount 15%, then add 8% tax on the discounted price. What is the final price? Show the steps.",
				},
				{
					ID:    "reason-plan",
					Label: "Planning",
					Text:  "You have 90 minutes to evaluate three local models for coding. Propose a timed plan with concrete checks for speed, quality, and memory pressure.",
				},
			},
		},
		{
			ID:          "summarization",
			Name:        "Summarization",
			Description: "Compress longer text into accurate briefs.",
			Prompts: []contracts.BenchmarkPrompt{
				{
					ID:    "sum-tech",
					Label: "Tech brief",
					Text: `Summarize the following in 4 bullet points for an engineering manager:

Yggdrasil is a local AI control plane that installs runtimes, downloads GGUF models, and exposes chat plus an OpenAI-compatible API. Nodes can be paired over the LAN. Profiles bind models to orchestrators such as simple single-agent chat or a team pipeline. Performance metrics capture TTFT and token evaluation speed so users can compare models on their own hardware.`,
				},
				{
					ID:    "sum-meeting",
					Label: "Meeting notes",
					Text: `Turn these notes into a short action list with owners implied when possible:

- Discussed model download sizes; prefer 1–3B for laptops
- Need a reset path for onboarding
- Diagnostics logs helped debug hung profiles list
- Want benchmark tool across coding/writing workloads
- Dark theme is the brand reference`,
				},
			},
		},
	}
}

// WorkloadByID returns a workload or false.
func WorkloadByID(id string) (contracts.BenchmarkWorkload, bool) {
	for _, w := range Workloads() {
		if w.ID == id {
			return w, true
		}
	}
	return contracts.BenchmarkWorkload{}, false
}
