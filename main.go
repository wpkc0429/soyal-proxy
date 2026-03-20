package main

import (
	"log"
	"os"
	"os/signal"
	"soyal-proxy/config"
	"soyal-proxy/publisher"
	"soyal-proxy/serialworker"
	"syscall"
)

func main() {
	log.Println("Starting SOYAL Proxy...")

	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config.json: %v", err)
	}

	pub, err := publisher.NewRedisPublisher(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize Redis Publisher: %v", err)
	}
	log.Println("Redis Publisher initialized.")

	worker, err := serialworker.NewWorker(cfg, pub)
	if err != nil {
		log.Fatalf("Failed to initialize Serial Worker on %s: %v", cfg.SerialPort, err)
	}
	log.Println("Serial Worker initialized. Connected to", cfg.SerialPort)

	worker.Start()

	// Wait for OS interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down SOYAL Proxy...")
}
