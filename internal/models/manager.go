package models

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yeixio/yggdrasil-core/internal/events"
	"github.com/yeixio/yggdrasil-core/pkg/contracts"
)

// Manager coordinates catalog, downloads, and installed state.
type Manager struct {
	catalog    *Catalog
	storage    *Storage
	downloader *Downloader
	bus        *events.Bus
	presets    []PurposePreset

	mu          sync.Mutex
	downloading map[string]struct{}
}

// NewManager constructs a model manager.
func NewManager(catalog *Catalog, storage *Storage, downloader *Downloader, bus *events.Bus) *Manager {
	return &Manager{
		catalog:     catalog,
		storage:     storage,
		downloader:  downloader,
		bus:         bus,
		downloading: make(map[string]struct{}),
	}
}

// SetPresets attaches purpose presets for recommendation ordering.
func (m *Manager) SetPresets(presets []PurposePreset) {
	m.presets = presets
}

// Presets returns purpose presets.
func (m *Manager) Presets() []PurposePreset { return m.presets }

// Catalog returns the underlying catalog.
func (m *Manager) Catalog() *Catalog { return m.catalog }

// Storage returns persistence helpers.
func (m *Manager) Storage() *Storage { return m.storage }

// SyncDynamic merges DB catalog rows into the in-memory catalog (HF installs).
func (m *Manager) SyncDynamic(ctx context.Context) {
	entries, err := m.storage.ListDynamicCatalog(ctx)
	if err != nil {
		return
	}
	for _, e := range entries {
		if _, ok := m.catalog.Get(e.ID); ok && !e.Dynamic {
			continue
		}
		m.catalog.Upsert(e)
	}
}

// List returns catalog models with installation status.
func (m *Manager) List(ctx context.Context) ([]contracts.Model, error) {
	m.SyncDynamic(ctx)
	installed, err := m.storage.ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	lastUsed, _ := m.storage.ListLastUsed(ctx)
	var out []contracts.Model
	for _, e := range m.catalog.List() {
		_, ok := installed[e.ID]
		status := "available"
		if m.isDownloading(e.ID) {
			status = "downloading"
		} else if ok {
			status = "installed"
		}
		model := m.storage.ToContract(e, ok, status)
		if t, ok := lastUsed[e.ID]; ok {
			tt := t
			model.LastUsedAt = &tt
		}
		out = append(out, model)
	}
	return out, nil
}

// Recommend wraps the recommendation engine with presets.
func (m *Manager) Recommend(ctx context.Context, purpose string, hw contracts.HardwareInventory, community map[string]CommunitySignal) (contracts.Recommendation, error) {
	in := RecommendInput{Purpose: purpose, Hardware: hw, Community: community}
	if len(m.presets) > 0 {
		return RecommendWithPresets(m.catalog, m.presets, in)
	}
	return Recommend(m.catalog, in)
}

// Fit builds hardware fit + winners for a node.
func (m *Manager) Fit(hw contracts.HardwareInventory, nodeID, nodeName string, opts FitOptions) contracts.ModelsFitResponse {
	return BuildFitResponse(m.catalog, hw, nodeID, nodeName, m.presets, opts)
}

// Install starts downloading a model by ID.
// When wait is true, the call blocks until the download finishes or ctx is cancelled.
func (m *Manager) Install(ctx context.Context, modelID string, wait bool) error {
	entry, ok := m.catalog.Get(modelID)
	if !ok {
		m.SyncDynamic(ctx)
		entry, ok = m.catalog.Get(modelID)
	}
	if !ok {
		return fmt.Errorf("model %q not in catalog", modelID)
	}
	if installed, _, err := m.storage.IsInstalled(ctx, modelID); err != nil {
		return err
	} else if installed {
		return nil
	}
	if err := m.storage.UpsertCatalogEntry(ctx, entry); err != nil {
		return err
	}
	m.mu.Lock()
	if _, ok := m.downloading[modelID]; ok {
		m.mu.Unlock()
		if !wait {
			return nil
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
				if installed, _, err := m.storage.IsInstalled(ctx, modelID); err != nil {
					return err
				} else if installed {
					return nil
				}
				if !m.isDownloading(modelID) {
					if installed, _, _ := m.storage.IsInstalled(ctx, modelID); installed {
						return nil
					}
					return fmt.Errorf("download of %q ended without installing", modelID)
				}
			}
		}
	}
	m.downloading[modelID] = struct{}{}
	m.mu.Unlock()

	if wait {
		defer func() {
			m.mu.Lock()
			delete(m.downloading, modelID)
			m.mu.Unlock()
		}()
		return m.downloader.Download(ctx, entry)
	}

	go func() {
		defer func() {
			m.mu.Lock()
			delete(m.downloading, modelID)
			m.mu.Unlock()
		}()
		_ = m.downloader.Download(context.Background(), entry)
	}()
	return nil
}

var nonID = regexp.MustCompile(`[^a-z0-9-]+`)

// InstallFromURL registers a dynamic catalog entry and starts download.
func (m *Manager) InstallFromURL(ctx context.Context, req contracts.InstallFromURLRequest, wait bool) (string, error) {
	src := strings.TrimSpace(req.SourceURL)
	if src == "" {
		return "", fmt.Errorf("source_url required")
	}
	if !strings.Contains(strings.ToLower(src), ".gguf") {
		return "", fmt.Errorf("only GGUF URLs are supported")
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = deriveIDFromURL(src, req.Filename, req.Variant)
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		name = id
	}
	filename := req.Filename
	if filename == "" {
		if u, err := url.Parse(src); err == nil {
			filename = path.Base(u.Path)
		}
	}
	entry := CatalogEntry{
		ID:                id,
		DisplayName:       name,
		Summary:           "Installed from Hugging Face browse",
		Variant:           req.Variant,
		Parameters:        req.Parameters,
		SizeBytes:         req.SizeBytes,
		MemoryNeededBytes: estimateMemory(req.SizeBytes),
		Capabilities:      contracts.ModelCapabilities{},
		Source:            contracts.ModelSource{URL: src, Format: "gguf"},
		Purpose:           []string{"general", "assistant"},
		Tags:              req.Tags,
		Runtime:           []string{"llamacpp"},
		Dynamic:           true,
	}
	if len(entry.Tags) == 0 {
		entry.Tags = []string{"general"}
	}
	m.catalog.Upsert(entry)
	if err := m.storage.UpsertCatalogEntry(ctx, entry); err != nil {
		return "", err
	}
	_ = filename
	if err := m.Install(ctx, id, wait); err != nil {
		return id, err
	}
	return id, nil
}

func deriveIDFromURL(src, filename, variant string) string {
	base := filename
	if base == "" {
		if u, err := url.Parse(src); err == nil {
			base = path.Base(u.Path)
		}
	}
	base = strings.TrimSuffix(strings.ToLower(base), ".gguf")
	if variant != "" {
		base = base + "-" + strings.ToLower(variant)
	}
	base = strings.ReplaceAll(base, "_", "-")
	base = nonID.ReplaceAllString(base, "-")
	for strings.Contains(base, "--") {
		base = strings.ReplaceAll(base, "--", "-")
	}
	return strings.Trim(base, "-")
}

func estimateMemory(sizeBytes uint64) uint64 {
	if sizeBytes == 0 {
		return 4 * 1024 * 1024 * 1024
	}
	return sizeBytes * 2
}

// Delete removes an installed model.
func (m *Manager) Delete(ctx context.Context, modelID string) error {
	m.downloader.Cancel(modelID)
	return m.storage.DeleteInstalled(ctx, modelID)
}

// Path returns the on-disk path for an installed model.
func (m *Manager) Path(ctx context.Context, modelID string) (string, error) {
	ok, path, err := m.storage.IsInstalled(ctx, modelID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", contracts.Errorf("MODEL_NOT_INSTALLED", map[string]any{"model_id": modelID}, "model %q not installed", modelID)
	}
	return path, nil
}

// StubModelID is the fake model used when TOSKAR_STUB_INFERENCE is enabled.
const StubModelID = "stub-team"

// EnsureStubModel registers and marks a tiny placeholder model as installed.
// Used by Docker cluster e2e so Team chat can run without downloading a GGUF.
func (m *Manager) EnsureStubModel(ctx context.Context) error {
	entry := CatalogEntry{
		ID:                StubModelID,
		DisplayName:       "Stub Team Model",
		Summary:           "CI/e2e placeholder — not a real GGUF",
		Family:            "stub",
		Variant:           "e2e",
		SizeBytes:         4,
		MemoryNeededBytes: 1,
		Context:           2048,
		// The stub says whatever a test scripts, including tool calls.
		Capabilities: contracts.ModelCapabilities{ToolCalling: true, ToolCallSupport: "compatible"},
		Purpose:      []string{"coding", "general"},
		Runtime:      []string{"llamacpp"},
		Dynamic:      true,
	}
	m.catalog.Upsert(entry)
	if err := m.storage.UpsertCatalogEntry(ctx, entry); err != nil {
		return err
	}
	path := m.storage.ModelPath(StubModelID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
		return err
	}
	return m.storage.MarkInstalled(ctx, StubModelID, path, "stub", 4)
}

// TouchLastUsed records activity for lifecycle and Installed tab.
func (m *Manager) TouchLastUsed(ctx context.Context, modelID string) {
	if entry, ok := m.catalog.Get(modelID); ok {
		_ = m.storage.UpsertCatalogEntry(ctx, entry)
	}
	_ = m.storage.TouchLastUsed(ctx, modelID, time.Now().UTC())
}

// Get returns a single model.
func (m *Manager) Get(ctx context.Context, modelID string) (contracts.Model, error) {
	m.SyncDynamic(ctx)
	entry, ok := m.catalog.Get(modelID)
	if !ok {
		return contracts.Model{}, fmt.Errorf("model %q not found", modelID)
	}
	installed, _, err := m.storage.IsInstalled(ctx, modelID)
	if err != nil {
		return contracts.Model{}, err
	}
	status := "available"
	if m.isDownloading(modelID) {
		status = "downloading"
	} else if installed {
		status = "installed"
	}
	return m.storage.ToContract(entry, installed, status), nil
}

func (m *Manager) isDownloading(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.downloading[id]
	return ok
}
