package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/yeixio/toskar-core/internal/screenshot"
)

func main() {
	webDir := flag.String("web", "web/dist", "built web UI directory")
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	flag.Parse()

	web := os.DirFS(*webDir)
	if _, err := os.Stat(*webDir + "/index.html"); err != nil {
		log.Fatalf("built web UI not found at %s (run the web build first)", *webDir)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("listening http://%s\n", ln.Addr())
	if err := http.Serve(ln, screenshot.Handler(web)); err != nil {
		log.Fatal(err)
	}
}
