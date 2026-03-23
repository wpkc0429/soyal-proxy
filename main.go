package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime"

	"soyal-proxy/api"
	"soyal-proxy/cli"
	"soyal-proxy/config"
	"soyal-proxy/database"
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

	// 修改資料庫路徑策略：由於跨 WSL 執行 Windows EXE 會遭遇 SMB 鎖死 (SQLITE_BUSY)
	// 若為 Windows 環境，將 events.db 強制寫入至本機 C 槽的 AppData 內以避開 UNC 路徑限制
	dbDir := "."
	if runtime.GOOS == "windows" || len(os.Getenv("LOCALAPPDATA")) > 0 {
		appData := os.Getenv("LOCALAPPDATA")
		if appData == "" {
			appData = os.TempDir()
		}
		dbDir = appData + "/soyal_proxy"
		os.MkdirAll(dbDir, 0755)
	}

	dbPath := dbDir + "/events.db"
	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	
	log.Printf("Initializing database at: %s", dbPath)
	err = database.InitDB(dsn, "global_users.json")
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	pub, err := publisher.NewRedisPublisher(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize Redis Publisher: %v", err)
	}
	log.Println("Redis Publisher initialized.")

	worker := serialworker.NewWorker(cfg, pub)
	if worker.IsOnline() {
		log.Println("Serial Worker initialized. Connected to", cfg.SerialPort)
	}

	// === 新增測試用的假刷卡資料 ===
	// worker.EventHistory = append(worker.EventHistory,
	// 	&parser.AccessEvent{DeviceName: "大門讀卡機", CardID: "12345:67890", Time: time.Now().Add(-15 * time.Minute), EventCode: 11, EventDesc: "Normal Access by tag"},
	// 	&parser.AccessEvent{DeviceName: "地下室車道", CardID: "00111:22222", Time: time.Now().Add(-5 * time.Minute), EventCode: 3, EventDesc: "Invalid card"},
	// 	&parser.AccessEvent{DeviceName: "資訊機房", CardID: "00000:00000", Time: time.Now().Add(-1 * time.Minute), EventCode: 17, EventDesc: "Alarm event"},
	// 	&parser.AccessEvent{DeviceName: "大門讀卡機", CardID: "00555:00001", Time: time.Now(), EventCode: 11, EventDesc: "Normal Access by tag"},
	// )

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
