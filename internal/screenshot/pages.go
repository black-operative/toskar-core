package screenshot

import (
	"strings"
	"time"
)

// Demo data for the Train, Knowledge, Memory, Tools, and Profiles pages
// (#304), so each looks in use. Like the rest of this package, it is made
// up: no real people, files, or accounts.

// Two specialized AIs: one deployed, one still being set up.
const screenshotAIs = `[
  {
    "id": "ai-trail",
    "slug": "trail-planner",
    "name": "Trail Planner",
    "goal": "Plan day hikes from our trail notes: distance, climb, and what to pack.",
    "instructions": "Answer with a short plan, then a packing list. Use miles and feet.",
    "base_model_id": "gemma-4-e4b",
    "preset": "balanced",
    "knowledge_sources": ["ks-trails"],
    "deployed_revision": 2,
    "created_at": "2026-09-12T09:00:00Z",
    "updated_at": "2026-09-23T16:40:00Z"
  },
  {
    "id": "ai-support",
    "slug": "shop-support",
    "name": "Shop Support",
    "goal": "Answer customer questions about orders and returns in our shop's tone.",
    "instructions": "Be brief and friendly. Offer the returns link when it helps.",
    "base_model_id": "qwen-coder-7b",
    "preset": "quick",
    "knowledge_sources": ["ks-returns"],
    "deployed_revision": 0,
    "created_at": "2026-09-22T11:00:00Z",
    "updated_at": "2026-09-24T10:15:00Z"
  }
]`

// A folder, a PDF, a spreadsheet, and a database, indexed for meaning search.
const screenshotKnowledge = `[
  {
    "id": "ks-trails",
    "name": "Trail notes",
    "kind": "path",
    "path": "~/Documents/Trail notes",
    "status": "ready",
    "chunk_count": 412,
    "embedded_count": 412,
    "embedding_model": "bge-m3",
    "language": "en",
    "languages": [{"language": "en", "passages": 412}],
    "created_at": "2026-09-10T08:00:00Z",
    "updated_at": "2026-09-24T07:00:00Z",
    "refreshed_at": "2026-09-24T07:00:00Z"
  },
  {
    "id": "ks-returns",
    "name": "Returns policy.pdf",
    "kind": "path",
    "path": "~/Documents/Shop/Returns policy.pdf",
    "filename": "Returns policy.pdf",
    "status": "ready",
    "chunk_count": 38,
    "embedded_count": 38,
    "embedding_model": "bge-m3",
    "language": "en",
    "created_at": "2026-09-21T15:00:00Z",
    "updated_at": "2026-09-21T15:02:00Z"
  },
  {
    "id": "ks-budget",
    "name": "Household budget.xlsx",
    "kind": "path",
    "path": "~/Documents/Household budget.xlsx",
    "filename": "Household budget.xlsx",
    "status": "ready",
    "chunk_count": 96,
    "embedded_count": 96,
    "embedding_model": "bge-m3",
    "language": "en",
    "local_only": true,
    "created_at": "2026-09-15T19:00:00Z",
    "updated_at": "2026-09-23T19:30:00Z"
  },
  {
    "id": "ks-orders",
    "name": "Shop orders",
    "kind": "database",
    "status": "indexing",
    "chunk_count": 1280,
    "embedded_count": 940,
    "embedding_model": "bge-m3",
    "language": "en",
    "created_at": "2026-09-24T13:50:00Z",
    "updated_at": "2026-09-24T14:05:00Z"
  }
]`

// A handful of memories, one kept on this computer only.
const screenshotMemory = `{
  "memories": [
    {"id": "mem-1", "content": "I prefer metric units for weather, and miles for hikes.", "category": "preferences", "source_type": "explicit", "enabled": true, "language": "en", "created_at": "2026-09-08T08:12:00Z", "updated_at": "2026-09-08T08:12:00Z"},
    {"id": "mem-2", "content": "My shop sells hand-dyed yarn and ships from Juneau.", "category": "projects", "source_type": "explicit", "enabled": true, "language": "en", "created_at": "2026-09-11T10:30:00Z", "updated_at": "2026-09-11T10:30:00Z"},
    {"id": "mem-3", "content": "The shop's website is built with Go and React.", "category": "technical", "source_type": "manual", "enabled": true, "language": "en", "created_at": "2026-09-14T17:05:00Z", "updated_at": "2026-09-14T17:05:00Z"},
    {"id": "mem-4", "content": "Our budget review is the first Sunday of each month.", "category": "other", "source_type": "explicit", "enabled": true, "local_only": true, "language": "en", "created_at": "2026-09-18T20:00:00Z", "updated_at": "2026-09-18T20:00:00Z"},
    {"id": "mem-5", "content": "Maya runs the shop's photos and social posts.", "category": "people", "source_type": "explicit", "enabled": true, "language": "en", "created_at": "2026-09-20T09:45:00Z", "updated_at": "2026-09-20T09:45:00Z"}
  ],
  "categories": ["identity", "preferences", "projects", "technical", "interests", "people", "other"]
}`

// GitHub connected, Home Assistant not.
const screenshotConnectors = `[
  {
    "id": "github",
    "name": "GitHub",
    "description": "Read issues and pull requests, and open them when you ask.",
    "scopes": "repo",
    "fields": [{"key": "token", "label": "Personal access token", "secret": true}],
    "connected": true,
    "status": "connected",
    "account": "yarn-shop",
    "connected_at": "2026-09-14T17:00:00Z",
    "checked_at": "2026-09-24T09:00:00Z",
    "tools": [
      {"id": "github.issues", "name": "List issues", "description": "List a repository's open issues.", "risk": "read", "default_policy": "allow"},
      {"id": "github.create_issue", "name": "Open an issue", "description": "Open an issue in a repository.", "risk": "write", "default_policy": "ask"}
    ]
  },
  {
    "id": "home-assistant",
    "name": "Home Assistant",
    "description": "Check and change devices in your Home Assistant.",
    "scopes": "",
    "fields": [
      {"key": "url", "label": "Home Assistant address", "secret": false, "placeholder": "http://homeassistant.local:8123"},
      {"key": "token", "label": "Long-lived access token", "secret": true}
    ],
    "connected": false,
    "tools": []
  }
]`

// One MCP tool source, connected and recently used.
const screenshotMCPServers = `[
  {
    "id": "mcp-notion",
    "name": "Notion",
    "description": "Search and read pages in your Notion workspace.",
    "preset": "notion",
    "where": "remote",
    "url": "https://mcp.notion.com/mcp",
    "env": [],
    "headers": [],
    "enabled": true,
    "allow_sampling": false,
    "always_offer": false,
    "keywords": ["notion", "notes", "wiki"],
    "status": "ready",
    "running": true,
    "signed_in": true,
    "server_name": "notion-mcp",
    "server_version": "1.4.2",
    "protocol": "2025-06-18",
    "has_resources": true,
    "has_prompts": false,
    "tools": [
      {"id": "mcp-notion.search", "name": "Search", "remote_name": "search", "description": "Search pages and databases.", "risk": "read", "policy": "allow", "changed": false, "enabled": true},
      {"id": "mcp-notion.fetch", "name": "Fetch page", "remote_name": "fetch", "description": "Read a page's content.", "risk": "read", "policy": "allow", "changed": false, "enabled": true},
      {"id": "mcp-notion.create-pages", "name": "Create pages", "remote_name": "create-pages", "description": "Create pages in a database.", "risk": "write", "policy": "ask", "changed": false, "enabled": true}
    ],
    "added_at": "2026-09-16T12:00:00Z",
    "checked_at": "2026-09-24T09:00:00Z",
    "last_used": "2026-09-24T13:42:00Z"
  }
]`

// screenshotAITrail is Trail Planner opened on the Train page: two finished
// revisions, the second deployed and exported, and a third training on the
// Studio. The running job started 22 minutes before the capture, so its
// elapsed time reads like a real one.
func screenshotAITrail() string {
	now := time.Now().UTC()
	return strings.NewReplacer(
		"{{jobCreated}}", now.Add(-24*time.Minute).Format(time.RFC3339),
		"{{jobStarted}}", now.Add(-22*time.Minute).Format(time.RFC3339),
	).Replace(screenshotAITrailTemplate)
}

const screenshotAITrailTemplate = `{
  "id": "ai-trail",
  "slug": "trail-planner",
  "name": "Trail Planner",
  "goal": "Plan day hikes from our trail notes: distance, climb, and what to pack.",
  "instructions": "Answer with a short plan, then a packing list. Use miles and feet.",
  "base_model_id": "gemma-4-e4b",
  "model_id": "trail-planner",
  "preset": "balanced",
  "knowledge_sources": ["ks-trails"],
  "deployed_revision": 2,
  "deployable_revisions": [1, 2],
  "created_at": "2026-09-12T09:00:00Z",
  "updated_at": "2026-09-24T14:10:00Z",
  "base_model": {"id": "gemma-4-e4b", "display_name": "Gemma 4 E4B", "parameters": "4B", "license": "Gemma", "installed": true},
  "materials": [
    {"id": "mat-1", "ai_id": "ai-trail", "name": "Past trip plans", "filename": "trip-plans.jsonl", "use": "training", "recommended": {"use": "training", "reasons": ["Question and answer pairs"], "example_count": 186, "can_train": true}, "example_count": 186, "created_at": "2026-09-12T09:20:00Z"},
    {"id": "mat-2", "ai_id": "ai-trail", "name": "Trail notes", "use": "knowledge", "recommended": {"use": "knowledge", "reasons": ["Reference notes that change over time"], "example_count": 0, "can_train": false}, "knowledge_source_id": "ks-trails", "example_count": 0, "created_at": "2026-09-12T09:25:00Z"}
  ],
  "dataset": {"total": 186, "usable": 181, "excluded": 5, "flagged": {"duplicate": 3, "short_answer": 2}, "tokens": 98400, "p95_tokens": 920},
  "revisions": [
    {"ai_id": "ai-trail", "revision": 1, "job_id": "job-1", "base_model_id": "gemma-4-e4b", "backend": "mlx", "hyper": {"method": "lora", "epochs": 2, "rank": 8}, "example_count": 142, "final_train_loss": 1.18, "final_val_loss": 1.31, "evaluated": true, "created_at": "2026-09-13T11:05:00Z"},
    {"ai_id": "ai-trail", "revision": 2, "job_id": "job-2", "base_model_id": "gemma-4-e4b", "backend": "mlx", "hyper": {"method": "lora", "epochs": 3, "rank": 16}, "example_count": 181, "final_train_loss": 0.86, "final_val_loss": 0.97, "evaluated": true, "created_at": "2026-09-23T16:30:00Z"}
  ],
  "jobs": [
    {"id": "job-3", "ai_id": "ai-trail", "revision": 3, "node_id": "studio", "node_name": "Studio", "backend": "mlx", "state": "training", "progress": {"iter": 410, "iters": 720, "epoch": 2, "epochs": 3, "train_loss": 0.91, "val_loss": 1.02, "tokens_per_sec": 1840, "peak_memory_gb": 11.2, "remaining_sec": 540}, "hyper": {"method": "lora", "epochs": 3, "rank": 16}, "created_at": "{{jobCreated}}", "started_at": "{{jobStarted}}"},
    {"id": "job-2", "ai_id": "ai-trail", "revision": 2, "node_id": "local", "node_name": "This Mac", "backend": "mlx", "state": "complete", "progress": {"iter": 680, "iters": 680, "epoch": 3, "epochs": 3, "train_loss": 0.86, "val_loss": 0.97}, "hyper": {"method": "lora", "epochs": 3, "rank": 16}, "created_at": "2026-09-23T15:40:00Z", "started_at": "2026-09-23T15:41:00Z", "finished_at": "2026-09-23T16:30:00Z"}
  ],
  "eval_runs": [
    {"id": "eval-2", "ai_id": "ai-trail", "revision": 2, "status": "complete", "results": [], "created_at": "2026-09-23T16:35:00Z", "finished_at": "2026-09-23T16:41:00Z"}
  ],
  "test_prompts": [
    {"id": "tp-1", "prompt": "Plan a 6-mile hike with about 2,000 feet of climb for Saturday."},
    {"id": "tp-2", "prompt": "What should I pack for a rainy ridge walk?"}
  ]
}`

const screenshotTrailExport = `{
  "ai_id": "ai-trail",
  "revision": 2,
  "state": "ready",
  "filename": "trail-planner-r2.gguf",
  "size_bytes": 2684354560,
  "created_at": "2026-09-23T17:02:00Z",
  "instructions": "Answer with a short plan, then a packing list. Use miles and feet."
}`

// Image generation set up with FLUX.2 [klein] 4B, for the Tools page.
const screenshotImageSetup = `{
  "supported": true,
  "ready": true,
  "program": true,
  "release": "master-6b1f8c2",
  "active": "flux2-klein-4b",
  "models": [
    {"id": "flux2-klein-4b", "name": "FLUX.2 [klein] 4B", "description": "Makes and edits images.", "license": "Apache-2.0", "memory_bytes": 5583457484, "size_bytes": 5583457484, "edits": true, "kind": "image", "installed": true, "recommended": true}
  ]
}`
