package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yeixio/toskar-core/internal/locale"
)

// Every turn knows the date and time where the person is.
func TestTurnKnowsDateAndTime(t *testing.T) {
	juneau, err := time.LoadLocation("America/Juneau")
	if err != nil {
		t.Skip("no time zone data")
	}
	at := time.Date(2026, 10, 3, 1, 42, 0, 0, time.UTC)
	if got, want := nowLine(at, juneau), "Friday, October 2, 2026, 5:42 PM AKDT (UTC-08:00, America/Juneau)"; !strings.Contains(got, want) {
		t.Fatalf("line = %q, want %q", got, want)
	}
	ctx := locale.WithTimeZone(context.Background(), "America/Juneau")
	env := &chatExecEnv{ctx: ctx, startedAt: at}
	if got := env.TurnInstructions(ctx, "what day is it?"); !strings.Contains(got, "Friday, October 2, 2026") {
		t.Fatalf("instructions = %q", got)
	}
	// An unknown zone keeps this computer's.
	if locale.TimeZone(locale.WithTimeZone(context.Background(), "Mars/Olympus")) != time.Local {
		t.Fatal("an unknown zone was used")
	}
}
