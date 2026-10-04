package gjallarhorn

import (
	"testing"

	"github.com/yeixio/toskar-core/internal/leakcheck"
)

// Goroutines the tests start must be gone when they end (#231).
func TestMain(m *testing.M) { leakcheck.Main(m) }
