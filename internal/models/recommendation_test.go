package models

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func testCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := NewCatalogEmbedded()
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return c
}

func TestRecommendCodingWithVRAM(t *testing.T) {
	c := testCatalog(t)
	rec, err := Recommend(c, RecommendInput{
		Purpose: "coding",
		Hardware: contracts.HardwareInventory{
			Memory: contracts.MemoryInfo{TotalBytes: 16 * 1024 * 1024 * 1024},
			Accelerators: []contracts.Accelerator{{
				Vendor:        "nvidia",
				DedicatedVRAM: 8 * 1024 * 1024 * 1024,
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Purpose != "coding" {
		t.Fatalf("purpose=%q", rec.Purpose)
	}
	if len(rec.Roles) < 2 {
		t.Fatalf("expected roles, got %d", len(rec.Roles))
	}
	if rec.Reason == "" {
		t.Fatal("expected reason")
	}
}

func TestRecommendLowMemoryFallsBack(t *testing.T) {
	c := testCatalog(t)
	rec, err := Recommend(c, RecommendInput{
		Purpose: "general",
		Hardware: contracts.HardwareInventory{
			Memory: contracts.MemoryInfo{TotalBytes: 4 * 1024 * 1024 * 1024},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Models) == 0 {
		t.Fatal("expected at least one model")
	}
}

func TestRecommendUnknownPurposeUsesCatalog(t *testing.T) {
	c := testCatalog(t)
	rec, err := Recommend(c, RecommendInput{
		Purpose: "unknown-purpose",
		Hardware: contracts.HardwareInventory{
			Memory: contracts.MemoryInfo{TotalBytes: 32 * 1024 * 1024 * 1024},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Models) == 0 {
		t.Fatal("expected models")
	}
}
