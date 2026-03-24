package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"gorm.io/gorm"
	"soyal-proxy/cli"
	"soyal-proxy/config"
	"soyal-proxy/database"
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
	http.HandleFunc("/api/sync-time", s.handleSyncTime)
	http.HandleFunc("/api/control", s.handleControl)
	http.HandleFunc("/api/events", s.handleEvents)
	http.HandleFunc("/api/history", s.handleHistory) // latest 100 in ram
	http.HandleFunc("/api/logs", s.handleLogs)       // permanent database history
	http.HandleFunc("/api/status", s.handleStatus)
	http.HandleFunc("/api/groups", s.handleGroups)

	go http.ListenAndServe(":8080", nil)
}

type UserPayload struct {
	CardID      string                              `json:"card_id"`
	Notes       string                              `json:"notes"`
	GroupIDs    []uint                              `json:"group_ids"`
	Permissions map[string]cli.GlobalPermission     `json:"permissions"`
}

type GroupPayload struct {
	ID          uint                                `json:"id"`
	Name        string                              `json:"name"`
	Permissions map[string]cli.GlobalPermission     `json:"permissions"`
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var dbUsers []database.User
		database.DB.Order("id desc").Find(&dbUsers)
		var users []UserPayload
		for _, du := range dbUsers {
			var perms map[string]cli.GlobalPermission
			if du.Permissions != "" {
				json.Unmarshal([]byte(du.Permissions), &perms)
			}
			if perms == nil {
				perms = make(map[string]cli.GlobalPermission)
			}
			var gids []uint
			if du.GroupIDs != "" {
				json.Unmarshal([]byte(du.GroupIDs), &gids)
			}
			if gids == nil {
				gids = []uint{} // Ensure empty array instead of null
			}
			users = append(users, UserPayload{
				CardID:      du.CardID,
				Notes:       du.Notes,
				GroupIDs:    gids,
				Permissions: perms,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(users)
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}

		var users []UserPayload
		if err := json.Unmarshal(body, &users); err != nil {
			http.Error(w, "Invalid user json format", http.StatusBadRequest)
			return
		}

		// Save to SQLite
		database.DB.Transaction(func(tx *gorm.DB) error {
			tx.Exec("DELETE FROM users") // Empties table
			for _, u := range users {
				permBytes, _ := json.Marshal(u.Permissions)
				gidsBytes, _ := json.Marshal(u.GroupIDs)
				tx.Create(&database.User{
					CardID:      u.CardID,
					Notes:       u.Notes,
					GroupIDs:    string(gidsBytes),
					Permissions: string(permBytes),
				})
			}
			return nil
		})

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
	
	go func() {
		err := s.worker.PerformSyncDownDB()
		if err != nil {
			log.Printf("SyncDown error: %v", err)
		}
	}()
	
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success", "message": "Global Sync DOWN started in background"}`))
}

func (s *Server) handleSyncUp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go func() {
		err := s.worker.PerformSyncUpDB()
		if err != nil {
			log.Printf("SyncUp error: %v", err)
		}
	}()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success", "message": "Global Sync UP started in background"}`))
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

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	ch := s.worker.SubscribeEvents()
	defer s.worker.UnsubscribeEvents(ch)

	for {
		select {
		case evt := <-ch:
			b, err := json.Marshal(evt)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", string(b))
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	history := s.worker.GetEventHistory()
	json.NewEncoder(w).Encode(history)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := s.worker.GetNodeStatus()
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	var dbLogs []database.AccessLog
	// default load 200 records
	database.DB.Order("time desc").Limit(200).Find(&dbLogs)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dbLogs)
}

func (s *Server) handleSyncTime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	err := s.worker.BroadcastTimeSync()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		var groups []database.Group
		database.DB.Order("id asc").Find(&groups)
		var resp []GroupPayload
		for _, g := range groups {
			var p map[string]cli.GlobalPermission
			if g.Permissions != "" {
				json.Unmarshal([]byte(g.Permissions), &p)
			}
			if p == nil {
				p = make(map[string]cli.GlobalPermission)
			}
			resp = append(resp, GroupPayload{ID: g.ID, Name: g.Name, Permissions: p})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		var groupsReq []GroupPayload
		if err := json.NewDecoder(r.Body).Decode(&groupsReq); err != nil {
			http.Error(w, "Invalid body", http.StatusBadRequest)
			return
		}
		database.DB.Transaction(func(tx *gorm.DB) error {
			tx.Exec("DELETE FROM groups")
			for _, g := range groupsReq {
				b, _ := json.Marshal(g.Permissions)
				tx.Create(&database.Group{
					ID: g.ID,
					Name: g.Name,
					Permissions: string(b),
				})
			}
			return nil
		})
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"success"}`))
		return
	}
}
