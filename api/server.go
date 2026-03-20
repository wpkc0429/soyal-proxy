package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"soyal-proxy/cli"
	"soyal-proxy/config"
	"soyal-proxy/publisher"
	"soyal-proxy/serialworker"
)

type Server struct {
	worker *serialworker.Worker
	cfg    *config.Config
}

func StartServer(worker *serialworker.Worker, cfg *config.Config) {
	s := &Server{worker: worker, cfg: cfg}

	http.Handle("/", http.FileServer(http.Dir("./web")))

	http.HandleFunc("/api/users", s.handleUsers)
	http.HandleFunc("/api/config", s.handleConfig)
	http.HandleFunc("/api/sync-down", s.handleSyncDown)
	http.HandleFunc("/api/sync-up", s.handleSyncUp)
	http.HandleFunc("/api/control", s.handleControl)

	go http.ListenAndServe(":8080", nil)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		data, err := os.ReadFile("global_users.json")
		if err != nil {
			if os.IsNotExist(err) {
				w.Write([]byte("[]"))
				return
			}
			http.Error(w, "Failed to read whitelist", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		// Expect the entire Array of GlobalUsers to be updated in bulk
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}

		var users []cli.GlobalUser
		if err := json.Unmarshal(body, &users); err != nil {
			http.Error(w, "Invalid user json format", http.StatusBadRequest)
			return
		}

		updatedData, _ := json.MarshalIndent(users, "", "  ")
		if err := os.WriteFile("global_users.json", updatedData, 0644); err != nil {
			http.Error(w, "Failed to write whitelist", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleSyncDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Disable background polling momentarily if needed? 
	// The port is shared. SerialWorker currently hogs the port.
	// Oh wait! If web server calls SyncDownAll, it will TRY to open the same serial port!
	// It will FAIL with Access Denied if Worker is running!
	// BIG ISSUE: serial_worker already holds the open Serial Port!
	// We must either send a message to SerialWorker to pause and release, 
	// OR use the Worker's open port directly!
	// Since Sync process uses port.Write/Read sequentially, we should ask Worker to pause polling,
	// or simply return an error that we cannot Sync while background proxy is running.
	// For this proxy, since we combined everything into one process, we must suspend Worker!
	
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"error", "message": "Manual command-line sync required or stopping worker needed."}`))
}

func (s *Server) handleSyncUp(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"error", "message": "Manual command-line sync required or stopping worker needed."}`))
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.cfg)
}

func (s *Server) handleControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cmd publisher.ControlMessage
	if err := json.NewDecoder(r.Body).Decode(&cmd); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	log.Printf("Web UI requested control: %+v", cmd)
	s.worker.CommandChan <- cmd
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}
