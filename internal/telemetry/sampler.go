package telemetry

import (
	"context"
	"sync"
	"time"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Sampler reads this computer's figures every Busy interval while Busy
// says a model is loaded and every Idle interval otherwise. It keeps the
// readings of the last hour, and per-minute averages for the last day.
type Sampler struct {
	Reader Reader
	// Busy reports whether a model is loaded, so readings come often only
	// when they show something.
	Busy              func() bool
	BusyEvery, Idle   time.Duration
	RecentFor, DayFor time.Duration
	now               func() time.Time

	mu     sync.Mutex
	recent []contracts.LiveSample
	day    []contracts.LiveSample
	// minute collects this minute's readings for its average.
	minute []contracts.LiveSample
}

// NewSampler reads every 5 seconds while a model is loaded, every minute
// otherwise, and keeps an hour of readings and a day of minutes.
func NewSampler(r Reader, busy func() bool) *Sampler {
	return &Sampler{Reader: r, Busy: busy, BusyEvery: 5 * time.Second, Idle: time.Minute,
		RecentFor: time.Hour, DayFor: 24 * time.Hour, now: time.Now}
}

// Run reads until ctx ends.
func (s *Sampler) Run(ctx context.Context) {
	for {
		s.Add(s.read(ctx))
		wait := s.Idle
		if s.Busy != nil && s.Busy() {
			wait = s.BusyEvery
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (s *Sampler) read(ctx context.Context) contracts.LiveSample {
	r := s.Reader.Read(ctx)
	r.At = s.now().UTC()
	return r
}

// Add keeps a reading.
func (s *Sampler) Add(r contracts.LiveSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recent = append(s.recent, r)
	cut := r.At.Add(-s.RecentFor)
	i := 0
	for i < len(s.recent) && s.recent[i].At.Before(cut) {
		i++
	}
	s.recent = s.recent[i:]
	// A reading in a new minute closes the last one into the day.
	if n := len(s.minute); n > 0 && !sameMinute(s.minute[n-1].At, r.At) {
		s.day = append(s.day, average(s.minute))
		s.minute = nil
		dayCut := r.At.Add(-s.DayFor)
		j := 0
		for j < len(s.day) && s.day[j].At.Before(dayCut) {
			j++
		}
		s.day = s.day[j:]
	}
	s.minute = append(s.minute, r)
}

// Figures are the latest reading, the last hour, and the last day.
func (s *Sampler) Figures() (current contracts.LiveSample, recent, day []contracts.LiveSample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := len(s.recent); n > 0 {
		current = s.recent[n-1]
	}
	recent = append([]contracts.LiveSample{}, s.recent...)
	day = append([]contracts.LiveSample{}, s.day...)
	if len(s.minute) > 0 {
		day = append(day, average(s.minute))
	}
	return current, recent, day
}

func sameMinute(a, b time.Time) bool { return a.Truncate(time.Minute).Equal(b.Truncate(time.Minute)) }

// average is the mean of readings, at the start of their minute. A figure
// counts only where readings have it; one none of them has stays left out.
func average(rs []contracts.LiveSample) contracts.LiveSample {
	out := contracts.LiveSample{At: rs[0].At.Truncate(time.Minute)}
	out.CPUPercent = meanF(rs, func(r contracts.LiveSample) *float64 { return r.CPUPercent })
	out.MemoryUsedBytes = meanU(rs, func(r contracts.LiveSample) *uint64 { return r.MemoryUsedBytes })
	out.MemoryTotalBytes = meanU(rs, func(r contracts.LiveSample) *uint64 { return r.MemoryTotalBytes })
	// GPUs by position: a computer's cards don't change between readings.
	n := 0
	for _, r := range rs {
		n = max(n, len(r.GPUs))
	}
	for i := 0; i < n; i++ {
		var gs []contracts.GPUSample
		for _, r := range rs {
			if i < len(r.GPUs) {
				gs = append(gs, r.GPUs[i])
			}
		}
		g := contracts.GPUSample{Name: gs[0].Name}
		g.BusyPercent = meanGF(gs, func(g contracts.GPUSample) *float64 { return g.BusyPercent })
		g.TemperatureC = meanGF(gs, func(g contracts.GPUSample) *float64 { return g.TemperatureC })
		g.PowerWatts = meanGF(gs, func(g contracts.GPUSample) *float64 { return g.PowerWatts })
		g.MemoryUsedBytes = meanGU(gs, func(g contracts.GPUSample) *uint64 { return g.MemoryUsedBytes })
		g.MemoryTotalBytes = meanGU(gs, func(g contracts.GPUSample) *uint64 { return g.MemoryTotalBytes })
		out.GPUs = append(out.GPUs, g)
	}
	return out
}

func meanF(rs []contracts.LiveSample, get func(contracts.LiveSample) *float64) *float64 {
	var sum float64
	n := 0
	for _, r := range rs {
		if v := get(r); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return f64(sum / float64(n))
}

func meanU(rs []contracts.LiveSample, get func(contracts.LiveSample) *uint64) *uint64 {
	var sum, n uint64
	for _, r := range rs {
		if v := get(r); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return u64(sum / n)
}

func meanGF(gs []contracts.GPUSample, get func(contracts.GPUSample) *float64) *float64 {
	var sum float64
	n := 0
	for _, g := range gs {
		if v := get(g); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return f64(sum / float64(n))
}

func meanGU(gs []contracts.GPUSample, get func(contracts.GPUSample) *uint64) *uint64 {
	var sum, n uint64
	for _, g := range gs {
		if v := get(g); v != nil {
			sum += *v
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return u64(sum / n)
}
