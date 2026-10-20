// Command fakegithub runs the fake GitHub for dev/e2e login and previews. Test-only.
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
	// FAKE_RUN_STEP > 0 makes dispatched runs advance by themselves every step
	// (handy for Playwright and demos); 0 means tests advance runs manually.
	step := time.Duration(0)
	if v := os.Getenv("FAKE_RUN_STEP"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fatal: FAKE_RUN_STEP:", err)
			os.Exit(2)
		}
		step = d
	}
	fake := fakegithub.New()
	fake.SetRunStep(step)
	fmt.Println("fakegithub listening on", addr, "run step", step)
	srv := &http.Server{Addr: addr, Handler: fake, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
