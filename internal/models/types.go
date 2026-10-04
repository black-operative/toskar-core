package models

import "github.com/yeixio/toskar-core/pkg/contracts"

// CatalogEntry is a model in the curated manifest.
type CatalogEntry struct {
	ID                string                      `json:"id"`
	DisplayName       string                      `json:"display_name"`
	Summary           string                      `json:"summary,omitempty"`
	Family            string                      `json:"family,omitempty"`
	Variant           string                      `json:"variant,omitempty"`
	Parameters        string                      `json:"parameters,omitempty"`
	SizeBytes         uint64                      `json:"size_bytes,omitempty"`
	MemoryNeededBytes uint64                      `json:"memory_needed_bytes,omitempty"`
	Context           int                         `json:"context,omitempty"`
	Capabilities      contracts.ModelCapabilities `json:"capabilities"`
	Source            contracts.ModelSource       `json:"source"`
	Purpose           []string                    `json:"purpose,omitempty"`
	Tags              []string                    `json:"tags,omitempty"`
	Runtime           []string                    `json:"runtime,omitempty"`
	RecommendedRoles  []string                    `json:"recommended_roles,omitempty"`
	Dynamic           bool                        `json:"dynamic,omitempty"`
	// SupportRole marks an embedding, reranker, or classifier model.
	SupportRole string `json:"support_role,omitempty"`
	// Training is set when the model can be specialized with LoRA training.
	Training *TrainingInfo `json:"training,omitempty"`
	// Languages are how well the model writes each language it has a level
	// for (multilingual spec §13–14), with confidence and sources.
	Languages []contracts.LanguageCapability `json:"languages,omitempty"`
}

// Supporting reports an embedding, reranker, or classifier model. It serves
// Yggdrasil and never chats, so it is never recommended for chat.
func (e CatalogEntry) Supporting() bool { return e.SupportRole != "" }

// chatEntries leaves supporting models out.
func chatEntries(list []CatalogEntry) []CatalogEntry {
	out := make([]CatalogEntry, 0, len(list))
	for _, e := range list {
		if !e.Supporting() {
			out = append(out, e)
		}
	}
	return out
}

// TrainingInfo describes the trainable weights behind a catalog GGUF.
type TrainingInfo struct {
	// BaseRepo is the Hugging Face repository the GGUF was converted from.
	// LoRA training reads these weights; the adapter then applies to the GGUF.
	BaseRepo string `json:"base_repo"`
	// QuantizedRepo is a 4-bit MLX copy of the same weights, used for QLoRA.
	QuantizedRepo  string `json:"quantized_repo,omitempty"`
	Architecture   string `json:"architecture"`
	License        string `json:"license"`
	LicenseNote    string `json:"license_note,omitempty"`
	HiddenSize     int    `json:"hidden_size"`
	Layers         int    `json:"layers"`
	VocabSize      int    `json:"vocab_size"`
	BaseBytes      uint64 `json:"base_bytes"`
	QuantizedBytes uint64 `json:"quantized_bytes,omitempty"`
}

// PurposePreset describes a use-case preset.
type PurposePreset struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	PreferredModels []string `json:"preferred_models"`
}

// DownloadState tracks an in-progress download.
type DownloadState struct {
	ID              string
	ModelID         string
	Status          string
	BytesDownloaded uint64
	BytesTotal      uint64
	TempPath        string
	Error           string
}
