package llamacpp

import (
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// fakeLlamaEnv, when set, makes the test binary act as llama-server: the
// value is a file to write the pid of a child it starts, so a test can check
// that stopping the model ends the child too.
const fakeLlamaEnv = "YGG_FAKE_LLAMA"

// runFakeLlama serves /health on --port and starts a child process, as a
// llama-server with helper processes would, then waits to be stopped.
func runFakeLlama() {
	if os.Getenv("YGG_FAKE_CHILD") == "1" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	port := ""
	for i, a := range os.Args {
		if a == "--port" && i+1 < len(os.Args) {
			port = os.Args[i+1]
		}
	}
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "YGG_FAKE_CHILD=1")
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	_ = os.WriteFile(os.Getenv(fakeLlamaEnv), []byte(strconv.Itoa(child.Process.Pid)), 0o600)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	_ = http.ListenAndServe("127.0.0.1:"+port, mux)
	os.Exit(1)
}
