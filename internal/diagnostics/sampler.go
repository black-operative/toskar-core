package diagnostics

import (
	"context"
	"runtime"
	"sync"
	"time"
)

// RuntimeSample is the daemon's own memory and background work at a moment.
type RuntimeSample struct {
	At time.Time `json:"at"`
	// Goroutines are the daemon's background tasks.
	Goroutines int `json:"goroutines"`
	// HeapBytes is memory in use by Go objects; SysBytes is all the memory
	// the Go runtime holds from the system.
	HeapBytes uint64 `json:"heap_bytes"`
	SysBytes  uint64 `json:"sys_bytes"`
}

// RuntimeHistory is what the Diagnostics page shows.
type RuntimeHistory struct {
	StartedAt time.Time       `json:"started_at"`
	Now       RuntimeSample   `json:"now"`
	Samples   []RuntimeSample `json:"samples"`
	// Interval is how often a sample is kept, in seconds.
	Interval int `json:"interval_seconds"`
}

// Sampler keeps a day of the daemon's memory and goroutine counts, so a
// slow leak shows on the Diagnostics page and in reports (#231).
type Sampler struct {
	Interval time.Duration
	Keep     int

	mu      sync.Mutex
	started time.Time
	samples []RuntimeSample
}

// NewSampler samples every five minutes and keeps a day.
func NewSampler() *Sampler {
	return &Sampler{Interval: 5 * time.Minute, Keep: 288}
}

// Sample reads the current counts.
func Sample() RuntimeSample {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return RuntimeSample{At: time.Now().UTC(), Goroutines: runtime.NumGoroutine(), HeapBytes: m.HeapAlloc, SysBytes: m.Sys}
}

// Run samples until ctx ends.
func (s *Sampler) Run(ctx context.Context) {
	s.mu.Lock()
	s.started = time.Now().UTC()
	s.mu.Unlock()
	s.record(Sample())
	tick := time.NewTicker(s.Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.record(Sample())
		}
	}
}

func (s *Sampler) record(r RuntimeSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, r)
	if over := len(s.samples) - s.Keep; over > 0 {
		s.samples = append([]RuntimeSample(nil), s.samples[over:]...)
	}
}

// History is the kept samples and a fresh one.
func (s *Sampler) History() RuntimeHistory {
	s.mu.Lock()
	defer s.mu.Unlock()
	return RuntimeHistory{
		StartedAt: s.started,
		Now:       Sample(),
		Samples:   append([]RuntimeSample(nil), s.samples...),
		Interval:  int(s.Interval / time.Second),
	}
}
