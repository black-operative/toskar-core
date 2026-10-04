package nodes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
	"github.com/yeixio/toskar-core/pkg/pluginapi"
)

// Client calls authenticated internal node RPC.
type Client struct {
	baseURL  string
	http     *http.Client
	identity *auth.NodeIdentity
	peerID   string // the paired computer's node ID, each token's audience
}

// NewClient calls the paired computer peerID at baseURL, signing each
// request with identity.
func NewClient(baseURL string, identity *auth.NodeIdentity, peerID string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		identity: identity,
		peerID:   peerID,
		http:     &http.Client{Timeout: 120 * time.Second},
	}
}

// NewProbeClient is a short-timeout client for pairing / discovery probes.
func NewProbeClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

type NodeInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	CertPEM string `json:"cert_pem"`
}

func (c *Client) Health(ctx context.Context) error {
	_, err := c.HealthInfo(ctx)
	return err
}

// HealthInfo is the computer's health answer, including whether it is
// training.
func (c *Client) HealthInfo(ctx context.Context) (Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/v1/health", nil)
	if err != nil {
		return Health{}, err
	}
	c.applyAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return Health{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("health HTTP %d", resp.StatusCode)
	}
	var h Health
	// An older computer answers without training; that reads as not training.
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&h)
	return h, nil
}

func (c *Client) NodeInfo(ctx context.Context) (NodeInfo, error) {
	var info NodeInfo
	if err := c.getJSON(ctx, "/internal/v1/node", &info, false); err != nil {
		return info, err
	}
	return info, nil
}

func (c *Client) Hardware(ctx context.Context) (contracts.HardwareInventory, error) {
	var inv contracts.HardwareInventory
	if err := c.getJSON(ctx, "/internal/v1/hardware", &inv, true); err != nil {
		return inv, err
	}
	return inv, nil
}

func (c *Client) ListModels(ctx context.Context) ([]contracts.Model, error) {
	var out []contracts.Model
	if err := c.getJSON(ctx, "/internal/v1/models", &out, true); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListRunning(ctx context.Context) ([]contracts.RunningModelView, error) {
	var out []contracts.RunningModelView
	if err := c.getJSON(ctx, "/internal/v1/models/running", &out, true); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) InstallModel(ctx context.Context, modelID string, wait bool) error {
	body, _ := json.Marshal(map[string]any{"model_id": modelID, "wait": wait})
	return c.postJSON(ctx, "/internal/v1/models/install", body, nil, true)
}

func (c *Client) InstallFromURL(ctx context.Context, req contracts.InstallFromURLRequest) (string, error) {
	body, _ := json.Marshal(req)
	var out struct {
		ModelID string `json:"model_id"`
	}
	if err := c.postJSON(ctx, "/internal/v1/models/install-from-url", body, &out, true); err != nil {
		return "", err
	}
	return out.ModelID, nil
}

func (c *Client) DeleteModel(ctx context.Context, modelID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/internal/v1/models/"+modelID, nil)
	if err != nil {
		return err
	}
	c.applyAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (c *Client) StartModel(ctx context.Context, modelID string) (contracts.RunningModelView, error) {
	var out contracts.RunningModelView
	if err := c.postJSON(ctx, "/internal/v1/models/"+modelID+"/start", []byte("{}"), &out, true); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) StopInstance(ctx context.Context, instanceID string) error {
	return c.postJSON(ctx, "/internal/v1/models/instances/"+instanceID+"/stop", []byte("{}"), nil, true)
}

func (c *Client) SendPairingOffer(ctx context.Context, offer auth.PairingOffer) error {
	body, _ := json.Marshal(offer)
	return c.postJSON(ctx, "/internal/v1/pairing/offer", body, nil, false)
}

// SendControlPairingOffer posts an offer to the peer control-plane API (port 7331).
func (c *Client) SendControlPairingOffer(ctx context.Context, offer auth.PairingOffer) error {
	body, _ := json.Marshal(offer)
	return c.postJSON(ctx, "/api/v1/nodes/pairing/offer", body, nil, false)
}

// FetchOutboundOffer pulls a pending outbound offer by code from a peer.
func (c *Client) FetchOutboundOffer(ctx context.Context, code string) (auth.PairingOffer, error) {
	var offer auth.PairingOffer
	if err := c.getJSON(ctx, "/internal/v1/pairing/outbound/"+code, &offer, false); err != nil {
		return offer, err
	}
	return offer, nil
}

// FetchOutboundOfferControl pulls an outbound offer via the peer control API.
func (c *Client) FetchOutboundOfferControl(ctx context.Context, code string) (auth.PairingOffer, error) {
	var offer auth.PairingOffer
	if err := c.getJSON(ctx, "/api/v1/nodes/pairing/outbound/"+code, &offer, false); err != nil {
		return offer, err
	}
	return offer, nil
}

func (c *Client) CompletePairing(ctx context.Context, complete auth.PairingComplete) error {
	body, _ := json.Marshal(complete)
	return c.postJSON(ctx, "/internal/v1/pairing/complete", body, nil, false)
}

// RemoteChatRequest is sent to a peer for generation.
type RemoteChatRequest struct {
	ModelID           string                  `json:"model_id"`
	Messages          []pluginapi.ChatMessage `json:"messages"`
	Temperature       float64                 `json:"temperature,omitempty"`
	MaxTokens         int                     `json:"max_tokens,omitempty"`
	Role              string                  `json:"role,omitempty"`
	RequesterNodeID   string                  `json:"requester_node_id,omitempty"`
	RequesterNodeName string                  `json:"requester_node_name,omitempty"`
}

func (c *Client) Chat(ctx context.Context, req RemoteChatRequest) (<-chan pluginapi.ChatChunk, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/internal/v1/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")
	c.applyAuth(httpReq)
	// Streaming may exceed default client timeout.
	streamClient := &http.Client{Timeout: 0}
	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	ch := make(chan pluginapi.ChatChunk, 16)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var chunk pluginapi.ChatChunk
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
				return
			}
			ch <- chunk
			if chunk.Done || chunk.Error != "" {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- pluginapi.ChatChunk{Error: err.Error(), Done: true}
		}
	}()
	return ch, nil
}

func (c *Client) getJSON(ctx context.Context, path string, v any, auth bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if auth {
		c.applyAuth(req)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c *Client) postJSON(ctx context.Context, path string, body []byte, out any, auth bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth {
		c.applyAuth(req)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	if out == nil {
		_, err := io.Copy(io.Discard, resp.Body)
		return err
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Do sends an authenticated request to path, such as
// "/internal/v1/training/runs", and returns the raw response. The caller
// closes the body. A long-running transfer should use a context deadline;
// the client timeout is lifted for it.
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.applyAuth(req)
	hc := *c.http
	hc.Timeout = 0
	return hc.Do(req)
}

func (c *Client) applyAuth(req *http.Request) {
	if c.identity != nil {
		req.Header.Set("Authorization", "Bearer "+c.identity.AuthToken(c.peerID))
	}
}
