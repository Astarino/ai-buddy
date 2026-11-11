package main

import (
	"github.com/Astarino/ai-buddy/internal/app"
	"log/slog"
	"os"
)

func main() {
	if err := app.Run("server"); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
