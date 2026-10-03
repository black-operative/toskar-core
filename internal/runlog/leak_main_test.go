package runlog

import (
	"testing"

	"github.com/yeixio/yggdrasil-core/internal/leakcheck"
)

// Goroutines the tests start must be gone when they end (#231).
func TestMain(m *testing.M) { leakcheck.Main(m) }
