package training

import (
	"context"
	"embed"
	"fmt"

	"github.com/yeixio/toskar-core/internal/models"
)

//go:embed samples/*
var sampleFS embed.FS

// SampleFile is example material people can read, download, or load.
type SampleFile struct {
	Filename    string `json:"filename"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

var sampleMeta = []SampleFile{
	{Filename: "tire-chats.jsonl", Name: "Example chats",
		Description: "Conversations in JSONL chat format, one per line. They teach the assistant to ask for the vehicle, explain fitment, and look up facts instead of quoting them. Three are flawed on purpose."},
	{Filename: "inventory.csv", Name: "Tire inventory",
		Description: "A product table with prices and stock counts. Values like these change, so they belong in connected knowledge, not in training."},
	{Filename: "returns-and-services.md", Name: "Policies and services",
		Description: "A Markdown document with returns, installation, and hours. Reference text like this is connected knowledge the assistant can quote."},
}

// Samples returns the example material.
func Samples() ([]SampleFile, error) {
	out := make([]SampleFile, 0, len(sampleMeta))
	for _, m := range sampleMeta {
		b, err := sampleFS.ReadFile("samples/" + m.Filename)
		if err != nil {
			return nil, err
		}
		m.Content = string(b)
		out = append(out, m)
	}
	return out, nil
}

const (
	exampleName = "Tread Right Tires (example)"
	exampleGoal = "Help customers of Tread Right Tires choose tires. Ask for the vehicle's year, make, and model, explain fitment, and look up current prices, stock, and policies instead of guessing."
)

// CreateExample builds the example AI from the sample material, the same
// way a person would: each file is classified and added with the
// recommended use. It returns the existing example if there is one.
func (s *Service) CreateExample(ctx context.Context, catalog []models.CatalogEntry) (SpecializedAI, error) {
	ais, err := s.d.Repo.ListAIs(ctx)
	if err != nil {
		return SpecializedAI{}, err
	}
	for _, ai := range ais {
		if ai.Example {
			return ai, nil
		}
	}
	base := ""
	if choices, err := s.RecommendBases(ctx, exampleGoal, catalog); err == nil {
		for _, c := range choices {
			if c.Fit.Eligible {
				base = c.ModelID
				break
			}
		}
	}
	ai, err := s.d.Repo.CreateAI(ctx, SpecializedAI{Name: exampleName, Goal: exampleGoal,
		Instructions: DraftInstructions("Tread Right Tires' assistant", exampleGoal), BaseModelID: base,
		Preset: PresetQuick, Example: true})
	if err != nil {
		return SpecializedAI{}, err
	}
	files, err := Samples()
	if err != nil {
		return SpecializedAI{}, err
	}
	for _, f := range files {
		if _, err := s.AddMaterial(ctx, ai.ID, MaterialInput{Name: f.Name, Filename: f.Filename, Text: f.Content}); err != nil {
			_ = s.DeleteAI(ctx, ai.ID)
			return SpecializedAI{}, fmt.Errorf("add %s: %w", f.Filename, err)
		}
	}
	return s.d.Repo.GetAI(ctx, ai.ID)
}
