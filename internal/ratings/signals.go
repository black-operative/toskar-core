package ratings

import (
	"context"

	"github.com/yeixio/toskar-core/internal/models"
)

// confidenceWeight is how much a cohort's ratings count in recommendations:
// early ratings half, a community's fully. Limited ones are never published.
var confidenceWeight = map[string]float64{"early": 0.5, "community": 1}

// Signals are community ratings as a recommendation signal, by local model
// ID: the narrowest published cohort of hardware like this computer's, or
// everyone's. They come from the summary already kept, so asking never
// sends anything; with community ratings off there are none.
func (s *Service) Signals(ctx context.Context) (map[string]models.CommunitySignal, error) {
	snap, ok, err := s.kept(ctx)
	if !ok || err != nil {
		return nil, err
	}
	inv, err := s.Hardware(ctx)
	if err != nil {
		return nil, err
	}
	h, hok := Normalize(inv)
	backend := Backend(inv)
	list, err := s.Models(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]models.CommunitySignal{}
	for _, m := range list {
		id, err := Identify(m, backend)
		if err != nil {
			continue
		}
		mc, ok := lookup(snap, id, h, hok)
		if !ok {
			continue
		}
		st, similar := mc.Similar, true
		if st == nil || confidenceWeight[st.Confidence] == 0 {
			st, similar = mc.Overall, false
		}
		if st == nil || confidenceWeight[st.Confidence] == 0 {
			continue
		}
		out[m.ID] = models.CommunitySignal{
			Signal:  (st.WeightedScore - snap.Prior) * confidenceWeight[st.Confidence],
			Score:   st.WeightedScore,
			Ratings: st.Ratings,
			Similar: similar,
		}
	}
	return out, nil
}
