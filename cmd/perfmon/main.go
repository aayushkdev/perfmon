package main

import (
	"context"
	"log"
	"time"

	"github.com/aayushkdev/perfmon/internal/backend"
	"github.com/aayushkdev/perfmon/internal/ui/tui"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := backend.NewCollector("/proc", "/sys")
	app := tui.NewApp(collector, 750*time.Millisecond)
	if err := app.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
