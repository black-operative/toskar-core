package llamacpp

import (
	"os"
	"testing"

	"github.com/yeixio/toskar-core/internal/leakcheck"
)

// The test binary doubles as a fake llama-server (see reap_unix_test.go).
// Goroutines the tests start must be gone when they end (#231).
func TestMain(m *testing.M) {
	if os.Getenv(fakeLlamaEnv) != "" {
		runFakeLlama()
		return
	}
	leakcheck.Main(m)
}
