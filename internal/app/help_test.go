package app

import (
	"strings"
	"testing"

	"github.com/yeixio/toskar-core/internal/locale"
)

func TestHelpTopic(t *testing.T) {
	cases := map[string]string{
		"how to install MPC???":                                "mcpTools",
		"How do I add an MCP server?":                          "mcpTools",
		"where do I set up tool sources?":                      "mcpTools",
		"How do I use Yggdrasil from Claude Desktop with MCP?": "mcpServer",
		"How do I use Toskar from Claude Desktop with MCP?":    "mcpServer",
		"How do I write an MCP server in Python?":              "", // a programming question, for the model
		"What is MCP?":              "", // not a how-to
		"How do I install Node.js?": "",
		"Can you summarize this article about the MCP spec and how it works, with examples of servers people use?": "",
	}
	for msg, want := range cases {
		if got := helpTopic(msg); got != want {
			t.Errorf("helpTopic(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestHelpNamesTheRealScreens(t *testing.T) {
	for _, lang := range []string{"en", "es", "ja", "zh-Hant"} {
		params := map[string]any{
			"tools": locale.T(lang, "common:nav.tools", nil), "addTools": locale.T(lang, "tools:sources.add", nil),
			"apiAccess": locale.T(lang, "common:nav.apiAccess", nil), "useElsewhere": locale.T(lang, "apiAccess:share.title", nil),
		}
		tools := locale.T(lang, "chat:help.mcpTools", params)
		if !strings.Contains(tools, params["tools"].(string)+" → "+params["addTools"].(string)) || strings.Contains(tools, "{{") {
			t.Errorf("%s: %q", lang, tools)
		}
		server := locale.T(lang, "chat:help.mcpServer", params)
		if !strings.Contains(server, params["useElsewhere"].(string)) || strings.Contains(server, "{{") {
			t.Errorf("%s: %q", lang, server)
		}
	}
}
