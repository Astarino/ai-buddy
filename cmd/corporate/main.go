package main

import (
	"github.com/Astarino/ai-buddy/internal/app"
	"log/slog"
	"os"
)

func main() {
	if err := app.Run("corporate"); err != nil {
		slog.Error("simulation stopped", "error", err)
		os.Exit(1)
	}
}
