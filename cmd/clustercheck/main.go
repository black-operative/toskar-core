package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/config"
)

const (
	nodeAID     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	nodeBID     = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	nodeCID     = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	stubModelID = "stub-team"
)

func main() {
	baseA := env("NODE_A_URL", "http://node-a:7331")
	baseB := env("NODE_B_URL", "http://node-b:7331")
	baseC := env("NODE_C_URL", "http://node-c:7331")
	internalB := env("NODE_B_INTERNAL", "http://node-b:7332")

	mustWait(baseA+"/api/v1/health", 90*time.Second)
	mustWait(baseB+"/api/v1/health", 90*time.Second)
	mustWait(baseC+"/api/v1/health", 90*time.Second)
	fmt.Println("all nodes healthy")

	mustRefresh(baseA)
	mustRefresh(baseB)
	mustRefresh(baseC)

	deadline := time.Now().Add(60 * time.Second)
	for {
		nodes := mustListNodes(baseA)
		if hasNode(nodes, nodeBID) && hasNode(nodes, nodeCID) {
			break
		}
		if time.Now().After(deadline) {
			fatalf("A never discovered B and C via static peers: %#v", nodes)
		}
		mustRefresh(baseA)
		time.Sleep(2 * time.Second)
	}
	fmt.Println("static peer discovery ok")

	pairAndApprove(baseA, baseB, nodeBID)
	pairAndApprove(baseA, baseC, nodeCID)
	fmt.Println("pairing ok")

	nodes := mustListNodes(baseA)
	if !paired(nodes, nodeBID) || !paired(nodes, nodeCID) {
		fatalf("expected B and C paired on A: %#v", nodes)
	}

	// Authenticated Bifrost path: list running models aggregates remotes.
	code, body := get(baseA + "/api/v1/models/running")
	if code != 200 {
		fatalf("models/running after pair: %d %s", code, body)
	}
	fmt.Println("authenticated remote inventory path ok")

	// Unsigned protected Bifrost route must fail.
	code, body = get(internalB + "/internal/v1/models")
	if code != 401 {
		fatalf("expected 401 without bearer, got %d %s", code, body)
	}
	fmt.Println("auth rejection ok")

	waitStubModel(baseA)
	waitStubModel(baseB)
	fmt.Println("stub model installed on A and B")

	runTeamDemo(baseA)
	fmt.Println("team multi-node demo ok")

	fmt.Println("cluster e2e passed")
}

func waitStubModel(base string) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		code, body := get(base + "/api/v1/models")
		if code == 200 {
			var models []map[string]any
			if err := json.Unmarshal([]byte(body), &models); err == nil {
				for _, m := range models {
					if m["id"] == stubModelID {
						if installed, _ := m["installed"].(bool); installed {
							return
						}
					}
				}
			}
		}
		if time.Now().After(deadline) {
			fatalf("stub model %q not installed on %s", stubModelID, base)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func runTeamDemo(baseA string) {
	profile := mustGETMap(baseA + "/api/v1/profiles/programming")
	profile["orchestrator_id"] = "team"
	profile["node_policy"] = map[string]any{"mode": "automatic"}
	profile["roles"] = []map[string]any{
		{"role": "coordinator", "model_id": stubModelID, "node_id": nodeAID, "required": false},
		{"role": "worker", "model_id": stubModelID, "node_id": nodeBID, "required": false},
		{"role": "reviewer", "model_id": stubModelID, "node_id": nodeAID, "required": false},
	}
	code, body := patch(baseA+"/api/v1/profiles/programming", profile)
	if code >= 400 {
		fatalf("patch programming profile: %d %s", code, body)
	}
	fmt.Println("programming profile pinned: worker → B")

	conv := mustPOST(baseA+"/api/v1/conversations", map[string]any{
		"title":      "team-e2e",
		"profile_id": "programming",
		"model_id":   stubModelID,
	})
	convID, _ := conv["id"].(string)
	if convID == "" {
		fatalf("conversation missing id: %#v", conv)
	}

	roleNodes := map[string]string{}
	var mu sync.Mutex
	done := make(chan struct{})
	go collectRoleEvents(baseA, roleNodes, &mu, done)

	// Give SSE a moment to subscribe before chat starts.
	time.Sleep(500 * time.Millisecond)

	code, body = post(baseA+"/api/v1/chat", map[string]any{
		"conversation_id": convID,
		"profile_id":      "programming",
		"model_id":        stubModelID,
		"message":         "Say hello in one short sentence.",
		"stream":          false,
	})
	if code >= 400 {
		close(done)
		fatalf("team chat failed: %d %s", code, body)
	}
	var chatResp map[string]any
	if err := json.Unmarshal([]byte(body), &chatResp); err != nil {
		close(done)
		fatalf("decode chat: %v (%s)", err, body)
	}
	content, _ := chatResp["content"].(string)
	if strings.TrimSpace(content) == "" {
		close(done)
		fatalf("expected one final chat answer, got empty: %s", body)
	}
	fmt.Println("team chat returned one final answer")

	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		workerNode := roleNodes["worker"]
		coordNode := roleNodes["coordinator"]
		reviewNode := roleNodes["reviewer"]
		mu.Unlock()
		if workerNode != "" && coordNode != "" && reviewNode != "" {
			break
		}
		if time.Now().After(deadline) {
			close(done)
			mu.Lock()
			fatalf("timed out waiting for orchestration.role events: %#v", roleNodes)
		}
		time.Sleep(200 * time.Millisecond)
	}
	close(done)

	mu.Lock()
	defer mu.Unlock()
	if roleNodes["worker"] != nodeBID {
		fatalf("expected worker on B (%s), got %s (roles=%#v)", nodeBID, roleNodes["worker"], roleNodes)
	}
	if roleNodes["coordinator"] != nodeAID {
		fatalf("expected coordinator on A (%s), got %s", nodeAID, roleNodes["coordinator"])
	}
	if roleNodes["reviewer"] != nodeAID {
		fatalf("expected reviewer on A (%s), got %s", nodeAID, roleNodes["reviewer"])
	}
	fmt.Printf("placement ok: coordinator=%s worker=%s reviewer=%s\n",
		roleNodes["coordinator"], roleNodes["worker"], roleNodes["reviewer"])
}

func collectRoleEvents(base string, roleNodes map[string]string, mu *sync.Mutex, done <-chan struct{}) {
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/events", nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var eventType string
	for {
		select {
		case <-done:
			return
		default:
		}
		if !scanner.Scan() {
			return
		}
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if eventType != "orchestration.role" {
			continue
		}
		var evt struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(raw), &evt); err != nil {
			continue
		}
		role, _ := evt.Payload["role"].(string)
		nodeID, _ := evt.Payload["node_id"].(string)
		if role == "" || nodeID == "" {
			continue
		}
		mu.Lock()
		roleNodes[role] = nodeID
		mu.Unlock()
		fmt.Printf("orchestration.role %s → %s\n", role, nodeID)
	}
}

func pairAndApprove(initiator, peer, remoteID string) {
	session := mustPOST(initiator+"/api/v1/nodes/pair", map[string]string{"node_id": remoteID})
	sid, _ := session["id"].(string)
	if sid == "" {
		fatalf("pair response missing id: %#v", session)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		pending := mustGETList(peer + "/api/v1/nodes/pairing/pending")
		found := false
		for _, p := range pending {
			if id, _ := p["id"].(string); id == sid {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			fatalf("pending offer %s never appeared on peer", sid)
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = mustPOST(peer+"/api/v1/nodes/"+sid+"/pair/approve", map[string]string{"code": ""})
	deadline = time.Now().Add(30 * time.Second)
	for {
		nodes := mustListNodes(initiator)
		if paired(nodes, remoteID) {
			return
		}
		if time.Now().After(deadline) {
			fatalf("node %s never became paired", remoteID)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func mustRefresh(base string) {
	code, body := post(base+"/api/v1/nodes/refresh", nil)
	if code >= 400 {
		fatalf("refresh %s: %d %s", base, code, body)
	}
}

func mustListNodes(base string) []map[string]any {
	return mustGETList(base + "/api/v1/nodes")
}

func mustWait(url string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		code, _ := get(url)
		if code == 200 {
			return
		}
		if time.Now().After(deadline) {
			fatalf("timeout waiting for %s", url)
		}
		time.Sleep(1 * time.Second)
	}
}

func hasNode(nodes []map[string]any, id string) bool {
	for _, n := range nodes {
		if n["id"] == id {
			return true
		}
	}
	return false
}

func paired(nodes []map[string]any, id string) bool {
	for _, n := range nodes {
		if n["id"] == id {
			p, _ := n["paired"].(bool)
			return p
		}
	}
	return false
}

func mustGETList(url string) []map[string]any {
	code, body := get(url)
	if code != 200 {
		fatalf("GET %s: %d %s", url, code, body)
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		fatalf("decode %s: %v (%s)", url, err, body)
	}
	return out
}

func mustGETMap(url string) map[string]any {
	code, body := get(url)
	if code != 200 {
		fatalf("GET %s: %d %s", url, code, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		fatalf("decode %s: %v (%s)", url, err, body)
	}
	return out
}

func mustPOST(url string, payload any) map[string]any {
	code, body := post(url, payload)
	if code >= 400 {
		fatalf("POST %s: %d %s", url, code, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		fatalf("decode POST %s: %v (%s)", url, err, body)
	}
	return out
}

func get(url string) (int, string) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err.Error()
	}
	applyControlAuth(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func post(url string, payload any) (int, string) {
	return doJSON(http.MethodPost, url, payload)
}

func patch(url string, payload any) (int, string) {
	return doJSON(http.MethodPatch, url, payload)
}

func doJSON(method, url string, payload any) (int, string) {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader([]byte("{}"))
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	applyControlAuth(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func applyControlAuth(req *http.Request) {
	if strings.Contains(req.URL.Path, "/internal/") {
		return
	}
	if key := strings.TrimSpace(config.Env("API_KEY")); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
