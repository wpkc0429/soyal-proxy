package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"soyal-proxy/api"
	"soyal-proxy/cli"
	"soyal-proxy/config"
	"soyal-proxy/publisher"
	"soyal-proxy/serialworker"
	"syscall"
)

func main() {
	syncDown := flag.Bool("sync-down-all", false, "Sync all user whitelists DOWN into global_users.json")
	syncUp := flag.Bool("sync-up-all", false, "Sync all user whitelists UP from global_users.json")
	flag.Parse()

	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Fatalf("Failed to load config.json: %v", err)
	}

	if *syncDown {
		if err := cli.SyncDownAll(cfg.SerialPort, cfg.BaudRate, cfg.Devices); err != nil {
			log.Println("SyncDownError:", err)
		}
		return
	}

	if *syncUp {
		if err := cli.SyncUpAll(cfg.SerialPort, cfg.BaudRate, cfg.Devices); err != nil {
			log.Println("SyncUpError:", err)
		}
		return
	}

	log.Println("Starting SOYAL Proxy...")

	pub, err := publisher.NewRedisPublisher(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize Redis Publisher: %v", err)
	}
	log.Println("Redis Publisher initialized.")

	worker := serialworker.NewWorker(cfg, pub)
	if worker.IsOnline() {
		log.Println("Serial Worker initialized. Connected to", cfg.SerialPort)
	}
	worker.Start()

	// Start Redis Subscriber to listen for remote control commands
	pub.StartSubscriber(worker.CommandChan)
	log.Println("Redis Subscriber listening on 'soyal_commands' topic.")

	// Start Web UI Server
	api.StartServer(worker, cfg)
	log.Println("Web Dashboard Server running on http://localhost:8080")

	// Wait for OS interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down SOYAL Proxy...")
}
