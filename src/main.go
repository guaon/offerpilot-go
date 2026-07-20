package main

import (
	"MyOfferPilot/src/env"
	"MyOfferPilot/src/logger"
	"MyOfferPilot/src/server"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	env.LoadEnvFile()

	logger.DefaultLogger.Info("OfferPilot starting...")

	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}

	srv := server.NewServer(port)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigChan
		logger.DefaultLogger.Info("Shutting down", map[string]interface{}{"signal": sig.String()})
		if err := srv.Stop(); err != nil {
			logger.DefaultLogger.Info("Failed to shutdown", map[string]interface{}{"error": err.Error()})
		}
	}()

	if err := srv.Start(); err != nil && err != http.ErrServerClosed {
		logger.DefaultLogger.Error("Server failed to start", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}
	logger.DefaultLogger.Info("Server stopped")
}
