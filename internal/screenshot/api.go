package screenshot

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

func serveScreenshotAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	path := r.URL.Path
	if path == "/api/v1/events" || strings.HasPrefix(path, "/api/v1/events/") {
		serveScreenshotEvents(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusOK, `{}`)
		return
	}
	if body, ok := screenshotGET(path); ok {
		writeJSON(w, http.StatusOK, body)
		return
	}
	writeJSON(w, http.StatusOK, `[]`)
}

func serveScreenshotEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": screenshot\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	<-r.Context().Done()
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func screenshotGET(path string) (string, bool) {
	switch {
	case path == "/api/v1/health":
		return `{"status":"ok","product":"Yggdrasil","version":"0.1.0","acceleration":"gpu"}`, true
	case path == "/api/v1/version":
		return `{"version":"0.1.0","commit":"screenshot","build_date":"2026-09-24T00:00:00Z","product":"Yggdrasil"}`, true
	case path == "/api/v1/hardware":
		return screenshotHardware, true
	case path == "/api/v1/models":
		return screenshotModels, true
	case path == "/api/v1/models/fit":
		return screenshotModelFit, true
	case path == "/api/v1/models/running":
		return screenshotRunning, true
	case strings.HasPrefix(path, "/api/v1/models/browse"):
		return `[]`, true
	case strings.HasPrefix(path, "/api/v1/models/recommend"):
		return `{"purpose":"general","roles":[],"models":[],"reason":"","estimated_storage_bytes":0,"estimated_vram_bytes":0}`, true
	case path == "/api/v1/profiles":
		return screenshotProfiles, true
	case path == "/api/v1/nodes":
		return screenshotNodes, true
	case path == "/api/v1/nodes/pairing/pending":
		return `[]`, true
	case path == "/api/v1/settings":
		return screenshotSettings, true
	case path == "/api/v1/conversations":
		return screenshotConversations, true
	case strings.HasPrefix(path, "/api/v1/conversations/") && strings.HasSuffix(path, "/messages"):
		return screenshotMessages, true
	case path == "/api/v1/api-keys":
		return screenshotAPIKeys, true
	case path == "/api/v1/runtimes":
		return screenshotRuntimes, true
	case path == "/api/v1/logs":
		return screenshotLogs, true
	case strings.HasPrefix(path, "/api/v1/logs/"):
		return screenshotLogBody, true
	// This Mac's last hour while a model answers, for the Performance page.
	case path == "/api/v1/performance/live":
		return screenshotLive(), true
	case strings.HasPrefix(path, "/api/v1/performance"):
		return screenshotPerformance, true
	case path == "/api/v1/tasks":
		return `[]`, true
	case path == "/api/v1/automations":
		return screenshotAutomations, true
	case strings.HasPrefix(path, "/api/v1/automations/"):
		return screenshotAutomationDetail, true
	case path == "/api/v1/tools":
		return screenshotTools, true
	case path == "/api/v1/benchmarks":
		return `[]`, true
	case path == "/api/v1/benchmarks/workloads":
		return `[]`, true
	case path == "/api/v1/mcp/share":
		return `{"url":"http://127.0.0.1:7331/mcp","command":"toskarctl","args":["mcp"],"needs_key":false}`, true
	case path == "/api/v1/capabilities":
		return screenshotCapabilities, true
	// The daemon's memory over a quiet day, for the Diagnostics page.
	case path == "/api/v1/diagnostics/runtime":
		return screenshotRuntimeHistory(), true
	// Setup status is an object, never a list: the Tools page reads its models.
	// Images are set up, so the Tools page shows a working setup; video isn't.
	case path == "/api/v1/images/setup":
		return screenshotImageSetup, true
	case path == "/api/v1/video/setup":
		return `{"supported":true,"ready":false,"program":false,"release":"","models":[]}`, true
	case path == "/api/v1/training/ais":
		return screenshotAIs, true
	case path == "/api/v1/training/ais/ai-trail":
		return screenshotAITrail(), true
	case path == "/api/v1/training/ais/ai-trail/revisions/2/export":
		return screenshotTrailExport, true
	case strings.HasPrefix(path, "/api/v1/training/ais/") && strings.HasSuffix(path, "/export"):
		return `{"state":"none"}`, true
	case path == "/api/v1/knowledge/sources":
		return screenshotKnowledge, true
	case path == "/api/v1/memory":
		return screenshotMemory, true
	case path == "/api/v1/connectors":
		return screenshotConnectors, true
	case path == "/api/v1/mcp/servers":
		return screenshotMCPServers, true
	case path == "/v1/models":
		return `{"object":"list","data":[{"id":"gemma-4-e4b","object":"model"}]}`, true
	default:
		return "", false
	}
}

const screenshotHardware = `{
  "os": "darwin",
  "arch": "arm64",
  "hostname": "studio",
  "cpu": {"model": "Apple M4 Pro", "cores": 14, "threads": 14},
  "memory": {"total_bytes": 51539607552, "available_bytes": 34359738368},
  "disk": {"path": "/", "total_bytes": 1000000000000, "available_bytes": 640000000000},
  "accelerators": [{
    "id": "gpu0",
    "vendor": "Apple",
    "model": "M4 Pro",
    "kind": "integrated",
    "unified_memory_bytes": 51539607552,
    "backends": ["metal"]
  }],
  "detected_at": "2026-09-24T12:00:00Z"
}`

const screenshotModels = `[
  {
    "id": "gemma-4-e4b",
    "display_name": "Gemma 4 E4B",
    "summary": "General assistant that fits on this Mac.",
    "family": "Gemma",
    "parameters": "4B",
    "size_bytes": 3200000000,
    "memory_needed_bytes": 4500000000,
    "context": 8192,
    "capabilities": {"tool_calling": true, "vision": false, "coding": true},
    "purpose": ["general", "coding"],
    "installed": true,
    "installed_on": [{"node_id": "local", "node_name": "This Mac"}],
    "status": "ready",
    "runtime": ["llamacpp"]
  },
  {
    "id": "qwen-coder-7b",
    "display_name": "Qwen Coder 7B",
    "summary": "Coding model installed on the studio Mac.",
    "family": "Qwen",
    "parameters": "7B",
    "size_bytes": 4800000000,
    "memory_needed_bytes": 7000000000,
    "context": 32768,
    "capabilities": {"tool_calling": true, "vision": false, "coding": true},
    "purpose": ["coding"],
    "installed": true,
    "installed_on": [{"node_id": "studio", "node_name": "Studio"}],
    "status": "ready",
    "runtime": ["llamacpp"]
  }
]`

const screenshotModelFit = `[{
  "node_id": "local",
  "node_name": "This Mac",
  "memory_bytes": 51539607552,
  "fits": [{
    "model_id": "gemma-4-e4b",
    "node_id": "local",
    "node_name": "This Mac",
    "label": "excellent",
    "expected_memory_bytes": 4500000000,
    "reason": "Fits comfortably in unified memory.",
    "est_tok_per_sec": 62
  }],
  "winners": [{"category": "general", "label": "Best overall", "model_id": "gemma-4-e4b"}]
}]`

const screenshotRunning = `[{
  "model_id": "gemma-4-e4b",
  "display_name": "Gemma 4 E4B",
  "instance_id": "run-1",
  "node_id": "local",
  "node_name": "This Mac",
  "status": "running",
  "memory_bytes": 4500000000,
  "speed_tok_per_sec": 58,
  "accelerator": "Apple M4 Pro",
  "used_by_profiles": ["General"],
  "acceleration": {"state": "gpu", "backend": "metal", "devices": ["Apple M4 Pro"], "layers_offloaded": 35, "layers_total": 35, "gpu_memory_bytes": 4800000000}
}]`

const screenshotProfiles = `[{
  "id": "general-assistant",
  "name": "General",
  "purpose": "general",
  "orchestrator_id": "simple",
  "roles": [{"role": "assistant", "model_id": "gemma-4-e4b", "node_id": "local", "required": true}],
  "node_policy": {"mode": "prefer_local"},
  "tools": []
}, {
  "id": "programming",
  "name": "Programming",
  "purpose": "coding",
  "orchestrator_id": "simple",
  "roles": [
    {"role": "planner", "model_id": "gemma-4-e4b", "node_id": "local", "required": true},
    {"role": "worker", "model_id": "qwen-coder-7b", "node_id": "studio", "required": true},
    {"role": "reviewer", "model_id": "gemma-4-e4b", "node_id": "local", "required": false}
  ],
  "node_policy": {"mode": "automatic", "preferred_nodes": ["studio"]},
  "orchestration": {"strategy": "team", "parallel": "on", "verification": "check"},
  "tools": [
    {"tool_id": "filesystem.read", "policy": "allow"},
    {"tool_id": "git.status", "policy": "allow"},
    {"tool_id": "git.commit", "policy": "ask"},
    {"tool_id": "terminal", "policy": "ask"}
  ]
}, {
  "id": "research",
  "name": "Research",
  "purpose": "research",
  "orchestrator_id": "simple",
  "roles": [{"role": "assistant", "model_id": "gemma-4-e4b", "node_id": "local", "required": true}],
  "node_policy": {"mode": "prefer_local"},
  "orchestration": {"strategy": "planned", "verification": "correct"},
  "tools": [{"tool_id": "internet.search", "policy": "allow"}, {"tool_id": "internet.open", "policy": "allow"}],
  "knowledge_sources": ["ks-trails"]
}]`

const screenshotNodes = `[
  {
    "id": "local",
    "name": "This Mac",
    "os": "darwin",
    "arch": "arm64",
    "status": "online",
    "is_local": true,
    "paired": true,
    "address": "127.0.0.1:7331",
    "hardware": {
      "os": "darwin",
      "arch": "arm64",
      "cpu": {"model": "Apple M4 Pro", "cores": 14},
      "memory": {"total_bytes": 51539607552, "available_bytes": 34359738368},
      "disk": {"path": "/", "total_bytes": 1000000000000, "available_bytes": 640000000000},
      "accelerators": [{"id": "gpu0", "vendor": "Apple", "model": "M4 Pro", "kind": "integrated", "unified_memory_bytes": 51539607552}]
    }
  },
  {
    "id": "studio",
    "name": "Studio",
    "os": "darwin",
    "arch": "arm64",
    "status": "online",
    "is_local": false,
    "paired": true,
    "address": "192.168.1.42:7331",
    "hardware": {
      "os": "darwin",
      "arch": "arm64",
      "cpu": {"model": "Apple M2 Max", "cores": 12},
      "memory": {"total_bytes": 68719476736, "available_bytes": 45097156608},
      "disk": {"path": "/", "total_bytes": 2000000000000, "available_bytes": 900000000000},
      "accelerators": [{"id": "gpu0", "vendor": "Apple", "model": "M2 Max", "kind": "integrated", "unified_memory_bytes": 68719476736}]
    }
  }
]`

const screenshotSettings = `{
  "data_dir": "/Users/demo/Library/Application Support/Toskar",
  "models_dir": "/Users/demo/Library/Application Support/Toskar/models",
  "runtimes_dir": "/Users/demo/Library/Application Support/Toskar/runtimes",
  "logs_dir": "/Users/demo/Library/Application Support/Toskar/logs",
  "api_host": "127.0.0.1",
  "api_port": 7331,
  "lan_api_enabled": false,
  "web_ui_enabled": true,
  "discovery_enabled": true,
  "node_name": "This Mac",
  "node_id": "local",
  "advanced_mode": true,
  "model_lifecycle": "automatic",
  "idle_unload_minutes": 15,
  "keep_running_in_background": true,
  "default_profile_id": "general-assistant",
  "default_execution": "automatic",
  "download_behavior": "ask",
  "model_storage_limit_gb": 50,
  "save_chat_history": true,
  "save_task_history": true,
  "notify_task_finish": true,
  "notify_peer_offline": true,
  "launch_at_login": false
}`

const screenshotConversations = `[{
  "id": "conv-local",
  "title": "Plan a weekend trail loop",
  "profile_id": "general-assistant",
  "model_id": "gemma-4-e4b",
  "created_at": "2026-09-24T15:00:00Z",
  "updated_at": "2026-09-24T15:04:00Z"
}]`

const screenshotMessages = `[
  {
    "id": "m1",
    "conversation_id": "conv-local",
    "role": "user",
    "content": "Suggest a half-day hike near the city that stays off the busiest trails.",
    "created_at": "2026-09-24T15:01:00Z"
  },
  {
    "id": "m2",
    "conversation_id": "conv-local",
    "role": "assistant",
    "content": "Take the ridge above the reservoir. It is about 6 miles, stays in the trees for the first hour, and the viewpoint is usually quiet by late morning. Pack water; there is no faucet after the trailhead.",
    "created_at": "2026-09-24T15:01:20Z"
  },
  {
    "id": "m3",
    "conversation_id": "conv-local",
    "role": "user",
    "content": "Can you make it a loop, with a good place to stop for lunch?",
    "created_at": "2026-09-24T15:03:00Z"
  },
  {
    "id": "m4",
    "conversation_id": "conv-local",
    "role": "assistant",
    "content": "Here is a loop that comes back past the creek:\n\n1. **Trailhead to the ridge**, 2.5 miles. The only steep part, through pines.\n2. **Ridge to the viewpoint**, 1 mile. Stop here for lunch: flat rocks, and shade after noon.\n3. **Down the creek trail**, 2.8 miles. An easy grade back to the car park.\n\nAbout **6.3 miles** and three and a half hours with lunch. Your notes say you prefer to start before 9, which keeps the climb in the shade.",
    "meta": {
      "sources": [
        {"kind": "knowledge", "title": "Ridge and creek loop", "source": "Trail notes", "snippet": "Creek trail: 2.8 mi, gentle grade, rejoins the car park."},
        {"kind": "memory", "title": "Hiking", "snippet": "Likes to start hikes before 9 am."}
      ]
    },
    "created_at": "2026-09-24T15:03:30Z"
  }
]`

const screenshotAPIKeys = `[
  {"id": "key-1", "name": "Editor", "prefix": "ygk_3f91", "created_at": "2026-09-01T00:00:00Z", "last_used_at": "2026-09-24T14:00:00Z", "revoked": false},
  {"id": "key-2", "name": "Notes app", "prefix": "ygk_88ab", "created_at": "2026-08-12T00:00:00Z", "revoked": false}
]`

const screenshotRuntimes = `[{
  "id": "llamacpp",
  "display_name": "llama.cpp (llama-server)",
  "status": "ready",
  "detection": {"installed": true, "version": "b11174", "path": "Contents/MacOS/llamacpp/llama-server"}
}]`

const screenshotLogs = `[{
  "name": "daemon.log",
  "kind": "daemon",
  "size_bytes": 4096,
  "modified_at": "2026-09-24T15:04:00Z",
  "label": "Service"
}]`

const screenshotLogBody = `{
  "name": "daemon.log",
  "kind": "daemon",
  "label": "Service",
  "content": "2026-09-24T15:04:00Z service ready on 127.0.0.1:7331\n2026-09-24T15:04:02Z model gemma-4-e4b running on This Mac\n",
  "truncated": false,
  "size_bytes": 128
}`

const screenshotPerformance = `[{
  "id": "gen-1",
  "conversation_id": "conv-local",
  "conversation_title": "Plan a weekend trail loop",
  "profile_name": "General",
  "model_id": "gemma-4-e4b",
  "runtime_id": "llamacpp",
  "prompt_tokens": 42,
  "completion_tokens": 68,
  "total_tokens": 110,
  "ttft_ms": 180,
  "prompt_ms": 90,
  "eval_ms": 1100,
  "total_ms": 1280,
  "prompt_tok_per_sec": 466,
  "eval_tok_per_sec": 61.8,
  "cross_machine": false,
  "node_count": 1,
  "created_at": "2026-09-24T15:01:20Z",
  "backend": "metal",
  "device": "Apple M4 Pro"
}]`

const screenshotAutomations = `[
  {
    "id": "price-1",
    "name": "Morning price",
    "enabled": true,
    "schedule": {"kind": "daily", "time_zone": "America/Los_Angeles", "hour": 8, "minute": 0},
    "prompt": "Check this product and report the price.",
    "profile_id": "general-assistant",
    "tools": [],
    "notification": {"mode": "condition", "condition": {"kind": "threshold", "op": "below", "value": 500}},
    "created_at": "2026-09-21T16:00:00Z",
    "updated_at": "2026-09-28T15:05:00Z",
    "next_run_at": "2026-09-29T15:00:00Z",
    "last_run_at": "2026-09-28T15:05:00Z",
    "consecutive_failures": 0,
    "last_status": "succeeded",
    "last_result": "The listing is $420."
  },
  {
    "id": "stock-1",
    "name": "Stock check",
    "enabled": false,
    "schedule": {"kind": "interval", "time_zone": "America/Los_Angeles", "every_seconds": 21600},
    "prompt": "Check whether this item is back in stock.",
    "profile_id": "general-assistant",
    "tools": [],
    "notification": {"mode": "condition", "condition": {"kind": "available"}},
    "created_at": "2026-09-22T16:00:00Z",
    "updated_at": "2026-09-24T16:00:00Z",
    "next_run_at": "2026-09-29T04:00:00Z",
    "last_run_at": "2026-09-28T22:00:00Z",
    "consecutive_failures": 0,
    "last_status": "succeeded",
    "last_result": "Still out of stock."
  },
  {
    "id": "friday-1",
    "name": "Friday releases",
    "enabled": true,
    "schedule": {"kind": "weekly", "time_zone": "America/Los_Angeles", "hour": 9, "minute": 0, "weekday": 5},
    "prompt": "Summarize the new releases.",
    "profile_id": "research",
    "tools": [],
    "notification": {"mode": "change"},
    "created_at": "2026-09-18T16:00:00Z",
    "updated_at": "2026-09-26T16:00:00Z",
    "next_run_at": "2026-10-02T16:00:00Z",
    "last_run_at": "2026-09-25T16:02:00Z",
    "consecutive_failures": 0,
    "last_status": "succeeded",
    "last_result": "Two patch releases, no behavior change."
  }
]`

const screenshotAutomationDetail = `{
  "id": "price-1",
  "name": "Morning price",
  "enabled": true,
  "schedule": {"kind": "daily", "time_zone": "America/Los_Angeles", "hour": 8, "minute": 0},
  "prompt": "Check this product and report the price.\n\nInclude a JSON object in the result with the numeric price, for example {\"price\": 420}.",
  "profile_id": "general-assistant",
  "tools": [],
  "notification": {"mode": "condition", "condition": {"kind": "threshold", "op": "below", "value": 500}},
  "created_at": "2026-09-21T16:00:00Z",
  "updated_at": "2026-09-28T15:05:00Z",
  "next_run_at": "2026-09-29T15:00:00Z",
  "last_run_at": "2026-09-28T15:05:00Z",
  "consecutive_failures": 0,
  "last_status": "succeeded",
  "last_result": "The listing is $420.",
  "history": [
    {
      "id": "run-2",
      "automation_id": "price-1",
      "occurrence_at": "2026-09-28T15:00:00Z",
      "status": "succeeded",
      "started_at": "2026-09-28T15:00:04Z",
      "finished_at": "2026-09-28T15:05:00Z",
      "result": "The listing is $420.\n{\"price\": 420}",
      "notification_sent": true,
      "model_id": "gemma-4-e4b",
      "node_id": "This Mac",
      "attempt": 1
    },
    {
      "id": "run-1",
      "automation_id": "price-1",
      "occurrence_at": "2026-09-27T15:00:00Z",
      "status": "succeeded",
      "started_at": "2026-09-27T15:00:03Z",
      "finished_at": "2026-09-27T15:04:00Z",
      "result": "The listing is $640.\n{\"price\": 640}",
      "notification_sent": false,
      "model_id": "gemma-4-e4b",
      "node_id": "This Mac",
      "attempt": 1
    }
  ]
}`

const screenshotTools = `[
  {"id": "internet.search", "name": "Web Search", "description": "Search the public internet and return titles, links, and snippets.", "capability": "internet", "source": "builtin", "schema": "{}", "default_policy": "allow", "risk": "read", "enabled": true, "profiles": ["general-assistant"]},
  {"id": "internet.open", "name": "Open Web Page", "description": "Open a web page and return readable text.", "capability": "internet", "source": "builtin", "schema": "{}", "default_policy": "allow", "risk": "read", "enabled": true, "profiles": ["general-assistant"]},
  {"id": "places.search", "name": "Find Places", "description": "Find places with OpenStreetMap, with addresses, opening hours, and distances.", "capability": "internet", "source": "builtin", "schema": "{}", "default_policy": "allow", "risk": "read", "enabled": true, "profiles": ["general-assistant"]},
  {"id": "files.create", "name": "Create File", "description": "Create a Word document or PDF the user can download.", "capability": "files", "source": "builtin", "schema": "{}", "default_policy": "allow", "risk": "write", "enabled": true, "profiles": ["general-assistant", "programming"]},
  {"id": "spreadsheet.analyze", "name": "Analyze Spreadsheet", "description": "Summarize a spreadsheet in this chat, column by column.", "capability": "files", "source": "builtin", "schema": "{}", "default_policy": "allow", "risk": "read", "enabled": true, "profiles": ["general-assistant"]},
  {"id": "git.commit", "name": "Git Commit", "description": "Create a git commit.", "capability": "git", "source": "builtin", "schema": "{}", "default_policy": "ask", "risk": "write", "enabled": true, "profiles": ["programming"]},
  {"id": "code.execute", "name": "Run Code", "description": "Run Python in a sandbox for calculations, data analysis, and charts.", "capability": "code", "source": "builtin", "schema": "{}", "default_policy": "ask", "risk": "execute", "enabled": false, "profiles": []},
  {"id": "github.issues", "name": "List issues", "description": "List a repository's open issues.", "capability": "connectors", "source": "connector:github", "schema": "{}", "default_policy": "allow", "risk": "read", "enabled": true, "profiles": ["programming"]},
  {"id": "mcp-notion.search", "name": "Search", "description": "Search pages and databases.", "capability": "mcp", "source": "mcp:Notion", "schema": "{}", "default_policy": "allow", "risk": "read", "enabled": true, "profiles": ["general-assistant"]}
]`

const screenshotCapabilities = `{
  "at": "2026-09-24T12:00:00Z",
  "models": [{"id": "gemma-4-e4b", "name": "Gemma 4 E4B", "running": true, "on": ["This Mac"]}],
  "nodes": [
    {"id": "local", "name": "This Mac", "local": true, "online": true, "trainer": "mlx"},
    {"id": "studio", "name": "Studio", "local": false, "online": true, "trainer": "mlx"}
  ],
  "tools": [
    {"id": "web.search", "name": "Web search", "source": "builtin", "enabled": true},
    {"id": "files.read", "name": "Read files", "source": "builtin", "enabled": true},
    {"id": "files.create", "name": "Create files", "source": "builtin", "enabled": true}
  ],
  "connectors": [],
  "providers": [{"id": "llamacpp", "name": "llama.cpp", "kind": "runtime", "status": "installed", "healthy": true}],
  "artifacts": {"count": 3, "bytes": 482304},
  "abilities": [
    {"id": "chat", "label": "Chat", "available": true, "via": ["Gemma 4 E4B"]},
    {"id": "web", "label": "Look things up on the web", "available": true, "via": ["Web search"]},
    {"id": "files", "label": "Read and create files", "available": true, "via": ["Read files", "Create files"]},
    {"id": "train", "label": "Train specialized AIs", "available": true, "via": ["MLX on This Mac", "MLX on Studio"]},
    {"id": "image", "label": "Generate images", "available": false, "note": "Install an image model to add this."}
  ]
}`

// screenshotRuntimeHistory is a day of steady memory, sampled every five
// minutes, as a healthy daemon looks.
func screenshotRuntimeHistory() string {
	start := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	var b strings.Builder
	b.WriteString(`{"started_at":"` + start.Format(time.RFC3339) + `","interval_seconds":300,"now":{"at":"` + start.Add(24*time.Hour).Format(time.RFC3339) + `","goroutines":42,"heap_bytes":31457280,"sys_bytes":62914560},"samples":[`)
	for i := range 288 {
		if i > 0 {
			b.WriteString(",")
		}
		heap := 30<<20 + (i%12)*256<<10
		fmt.Fprintf(&b, `{"at":"%s","goroutines":%d,"heap_bytes":%d,"sys_bytes":%d}`, start.Add(time.Duration(i)*5*time.Minute).Format(time.RFC3339), 40+i%3, heap, 60<<20)
	}
	b.WriteString("]}")
	return b.String()
}

// screenshotLive is an hour of readings every minute on an Apple silicon Mac
// answering now and then: CPU, memory, and the GPU's busy % and the shared
// memory it uses (no temperature or power, as on a real Mac).
func screenshotLive() string {
	start := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	sample := func(i int) string {
		busy := 8 + (i*37)%55
		cpu := 6 + (i*13)%22
		return fmt.Sprintf(`{"at":"%s","cpu_percent":%d,"memory_used_bytes":%d,"memory_total_bytes":51539607552,"gpus":[{"name":"Apple M4 Pro","busy_percent":%d,"memory_used_bytes":4800000000}]}`,
			start.Add(time.Duration(i)*time.Minute).Format(time.RFC3339), cpu, 20<<30+int64(i%7)<<28, busy)
	}
	var b strings.Builder
	b.WriteString(`[{"node_id":"local","node_name":"This Mac","current":` + sample(59) + `,"recent":[`)
	for i := range 60 {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(sample(i))
	}
	b.WriteString(`],"day":[]}]`)
	return b.String()
}
