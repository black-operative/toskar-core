package telemetry

import (
	"testing"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestSamplerKeepsAnHourAndADay(t *testing.T) {
	s := NewSampler(nil, nil)
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	// Two readings a minute for three hours; the GPU's temperature is
	// left out, as on a Mac.
	for i := 0; i < 360; i++ {
		at := start.Add(time.Duration(i) * 30 * time.Second)
		cpu := float64(i % 2 * 50) // 0, 50, 0, 50…
		s.Add(contracts.LiveSample{At: at, CPUPercent: f64(cpu), GPUs: []contracts.GPUSample{{Name: "Apple M2 Pro", BusyPercent: f64(cpu)}}})
	}
	current, recent, day := s.Figures()
	if !current.At.Equal(start.Add(359 * 30 * time.Second)) {
		t.Errorf("current at %v", current.At)
	}
	if len(recent) != 121 {
		t.Errorf("recent has %d readings, want an hour's (121)", len(recent))
	}
	if len(day) != 180 {
		t.Fatalf("day has %d minutes, want 180", len(day))
	}
	m := day[10]
	if !m.At.Equal(start.Add(10*time.Minute)) || m.CPUPercent == nil || *m.CPUPercent != 25 {
		t.Errorf("a minute's average: %+v", m)
	}
	if g := m.GPUs[0]; g.Name != "Apple M2 Pro" || *g.BusyPercent != 25 || g.TemperatureC != nil {
		t.Errorf("a minute's GPU: %+v", g)
	}
}

func TestSamplerDropsOldMinutes(t *testing.T) {
	s := NewSampler(nil, nil)
	s.DayFor = 10 * time.Minute
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		s.Add(contracts.LiveSample{At: start.Add(time.Duration(i) * time.Minute)})
	}
	if _, _, day := s.Figures(); len(day) > 11 {
		t.Errorf("day kept %d minutes, want at most 11", len(day))
	}
}
