// Command fakegithub runs the fake GitHub for dev/e2e login. Test-only.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ephematool/ephematool/internal/fakegithub"
)

func main() {
	addr := os.Getenv("FAKEGITHUB_ADDR")
	if addr == "" {
		addr = ":9999"
	}
	fmt.Println("fakegithub listening on", addr)
	srv := &http.Server{Addr: addr, Handler: fakegithub.New(), ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
