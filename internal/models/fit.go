package models

import (
	"sort"
	"strings"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// ScoreFits grades every catalog model against hardware.
func ScoreFits(catalog *Catalog, hw contracts.HardwareInventory, nodeID, nodeName string, opts FitOptions) []contracts.ModelFit {
	if catalog == nil {
		return nil
	}
	out := make([]contracts.ModelFit, 0, len(catalog.List()))
	for _, e := range catalog.List() {
		out = append(out, ClassifyFit(e, hw, nodeID, nodeName, opts))
	}
	return out
}

func estimateTokPerSec(e CatalogEntry, hw contracts.HardwareInventory, runtimeBytes, capacity uint64) float64 {
	// Rough heuristic: smaller models + discrete/Metal GPU are faster.
	// This is not a measurement. The UI labels it as estimated unless a benchmark overrode it.
	base := 80.0
	need := runtimeBytes
	if need == 0 {
		need = e.MemoryNeededBytes
	}
	if need > 0 {
		gb := float64(need) / (1024 * 1024 * 1024)
		base = 120.0 / (1 + gb/4)
	}
	accelBoost := 1.0
	for _, a := range hw.Accelerators {
		kind := strings.ToLower(a.Kind)
		for _, b := range a.Backends {
			b = strings.ToLower(b)
			if b == "cuda" || b == "metal" || b == "hip" {
				accelBoost = 1.6
			}
			if b == "metal" {
				accelBoost = 1.4
			}
		}
		if strings.Contains(kind, "gpu") {
			if accelBoost < 1.3 {
				accelBoost = 1.3
			}
		}
	}
	if capacity > 0 && need > 0 {
		headroom := float64(capacity) / float64(need)
		if headroom < 1 {
			accelBoost *= 0.7
		} else if headroom > 2 {
			accelBoost *= 1.1
		}
	}
	return base * accelBoost
}

// PickWinners selects category recommendation badges for Discover.
// Only models that can run on this machine are eligible. Heavy fits stay eligible.
// Within a preset's preferred models, community ratings can move a well
// rated model ahead of the curated first choice (see pickByCommunity).
func PickWinners(catalog *Catalog, fits []contracts.ModelFit, presets *[]PurposePreset, community map[string]CommunitySignal) []contracts.CategoryWinner {
	if catalog == nil {
		return nil
	}
	fitMap := map[string]contracts.ModelFit{}
	for _, f := range fits {
		fitMap[f.ModelID] = f
	}
	entries := chatEntries(catalog.List())
	eligible := func(e CatalogEntry) bool {
		f, ok := fitMap[e.ID]
		return ok && runnableFit(f.Label) && !e.Supporting()
	}

	var winners []contracts.CategoryWinner
	chosen := map[string]bool{}
	add := func(category, label, modelID string) {
		if modelID == "" {
			return
		}
		for _, w := range winners {
			if w.Category == category {
				return
			}
		}
		winners = append(winners, contracts.CategoryWinner{
			Category:        category,
			Label:           label,
			ModelID:         modelID,
			CommunityChosen: chosen[category],
		})
	}

	// Prefer preset order when available, weighed against community
	// ratings.
	preferFirstFit := func(category string, ids []string) string {
		var ok []string
		for _, id := range ids {
			if e, found := catalog.Get(id); found && eligible(e) {
				ok = append(ok, id)
			}
		}
		i, byCommunity := pickByCommunity(ok, community)
		if i < 0 {
			return ""
		}
		chosen[category] = byCommunity
		return ok[i]
	}

	if presets != nil {
		for _, p := range *presets {
			switch p.ID {
			case "coding":
				add("coding", "Best coding", preferFirstFit("coding", p.PreferredModels))
			case "general":
				add("general", "Best overall", preferFirstFit("general", p.PreferredModels))
			case "research":
				add("reasoning", "Best reasoning", preferFirstFit("reasoning", p.PreferredModels))
			}
		}
	}

	var smallest, largest, bestCoding, bestVision *CatalogEntry
	for i := range entries {
		e := &entries[i]
		if !eligible(*e) {
			continue
		}
		if smallest == nil || e.MemoryNeededBytes < smallest.MemoryNeededBytes {
			smallest = e
		}
		if largest == nil || e.MemoryNeededBytes > largest.MemoryNeededBytes {
			largest = e
		}
		if e.Capabilities.Coding || hasTag(*e, "coding") {
			if bestCoding == nil || e.MemoryNeededBytes > bestCoding.MemoryNeededBytes {
				// Prefer mid-large coding that still fits for quality; override with preset if set.
				bestCoding = e
			}
		}
		if e.Capabilities.Vision || hasTag(*e, "vision") {
			if bestVision == nil || e.MemoryNeededBytes > bestVision.MemoryNeededBytes {
				bestVision = e
			}
		}
	}

	if smallest != nil {
		add("fastest", "Fastest", smallest.ID)
		add("lowest_memory", "Lowest memory", smallest.ID)
	}
	if largest != nil {
		add("best_quality", "Best quality", largest.ID)
	}
	if bestCoding != nil {
		add("coding", "Best coding", bestCoding.ID)
	}
	if bestVision != nil {
		add("vision", "Best vision", bestVision.ID)
	}
	// best_overall: prefer general preset, else mid-size general
	if !hasWinner(winners, "best_overall") && !hasWinner(winners, "general") {
		var mid *CatalogEntry
		for i := range entries {
			e := &entries[i]
			if !eligible(*e) || hasTag(*e, "coding") {
				continue
			}
			if hasTag(*e, "general") || containsPurpose(*e, "general") || containsPurpose(*e, "assistant") {
				if mid == nil {
					mid = e
					continue
				}
				// Prefer Good/Excellent mid-size
				f := fitMap[e.ID]
				if f.Label == contracts.FitExcellent || f.Label == contracts.FitGood {
					mid = e
				}
			}
		}
		if mid != nil {
			add("best_overall", "Best overall", mid.ID)
		}
	} else if hasWinner(winners, "general") {
		for _, w := range winners {
			if w.Category == "general" {
				add("best_overall", "Best overall", w.ModelID)
				break
			}
		}
	}

	sort.SliceStable(winners, func(i, j int) bool {
		return winners[i].Category < winners[j].Category
	})
	return winners
}

func hasWinner(winners []contracts.CategoryWinner, category string) bool {
	for _, w := range winners {
		if w.Category == category {
			return true
		}
	}
	return false
}

func hasTag(e CatalogEntry, tag string) bool {
	for _, t := range e.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

func containsPurpose(e CatalogEntry, purpose string) bool {
	for _, p := range e.Purpose {
		if strings.EqualFold(p, purpose) {
			return true
		}
	}
	return false
}

// BuildFitResponse builds the /models/fit payload for one node.
func BuildFitResponse(catalog *Catalog, hw contracts.HardwareInventory, nodeID, nodeName string, presets []PurposePreset, opts FitOptions) contracts.ModelsFitResponse {
	fits := ScoreFits(catalog, hw, nodeID, nodeName, opts)
	capacity, _ := machineCapacity(hw)
	if capacity == 0 {
		capacity = effectiveMemory(hw)
	}
	return contracts.ModelsFitResponse{
		NodeID:      nodeID,
		NodeName:    nodeName,
		MemoryBytes: capacity,
		Fits:        fits,
		Winners:     PickWinners(catalog, fits, &presets, opts.Community),
	}
}
