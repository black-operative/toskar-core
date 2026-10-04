package events

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestEventsCarryTheContract(t *testing.T) {
	b := NewBus(2)
	_, ch := b.Subscribe()
	b.Publish(New("chat.token", map[string]any{"content": "hi"}))
	b.Publish(Event{Type: "plan.step"})
	for i := 0; i < 2; i++ {
		if evt := <-ch; evt.Contract != contracts.ContractVersion {
			t.Fatalf("event %d contract = %q", i, evt.Contract)
		}
	}
}
