package serialworker

import (
	"encoding/json"
	"fmt"
	"log"

	"gorm.io/gorm"
	"soyal-proxy/cli"
	"soyal-proxy/database"
)

// PerformSyncDownDB fetches all whitelists from connected nodes and writes them to SQLite.
func (w *Worker) PerformSyncDownDB() error {
	w.mu.Lock()
	port := w.port
	w.port = nil // Steal port to pause polling
	w.mu.Unlock()

	if port == nil {
		return fmt.Errorf("system is offline")
	}

	defer func() {
		w.mu.Lock()
		w.port = port
		w.mu.Unlock()
	}()

	w.mu.RLock()
	devices := make([]string, 0, len(w.activeNodes))
	for id := range w.activeNodes {
		devices = append(devices, id)
	}
	w.mu.RUnlock()

	// 1. Fetch from Hardware
	globalUsers := make(map[string]*cli.GlobalUser)
	for _, nodeStr := range devices {
		var nodeID byte
		fmt.Sscanf(nodeStr, "%d", &nodeID)

		nodePerms := cli.SyncDownNodePort(port, nodeID)

		for cardID, perm := range nodePerms {
			if globalUsers[cardID] == nil {
				globalUsers[cardID] = &cli.GlobalUser{
					CardID:      cardID,
					Permissions: make(map[string]cli.GlobalPermission),
				}
			}
			globalUsers[cardID].Permissions[nodeStr] = perm
		}
	}

	// 2. Fetch existing DB users
	var dbUsers []database.User
	database.DB.Find(&dbUsers)
	
	// 3. Merge Hardware into DB
	for _, dbUser := range dbUsers {
		if globalUsers[dbUser.CardID] == nil {
			var perms map[string]cli.GlobalPermission
			if dbUser.Permissions != "" {
				json.Unmarshal([]byte(dbUser.Permissions), &perms)
			}
			globalUsers[dbUser.CardID] = &cli.GlobalUser{
				CardID:      dbUser.CardID,
				Notes:       dbUser.Notes, // keep existing name/notes
				Permissions: perms,
			}
		} else {
            // retain notes
			globalUsers[dbUser.CardID].Notes = dbUser.Notes
		}
	}

	// 4. Save back to SQLite safely
	database.DB.Transaction(func(tx *gorm.DB) error {
		tx.Exec("DELETE FROM users")
		for _, u := range globalUsers {
			permBytes, _ := json.Marshal(u.Permissions)
			tx.Create(&database.User{
				CardID:      u.CardID,
				Notes:       u.Notes,
				Permissions: string(permBytes),
			})
		}
		return nil
	})

	log.Printf("Web Sync DOWN completed. Saved %d merged users to database.\n", len(globalUsers))
	return nil
}

// PerformSyncUpDB pushes all DB configurations to connected nodes.
func (w *Worker) PerformSyncUpDB() error {
	w.mu.Lock()
	port := w.port
	w.port = nil // Steal port to pause polling
	w.mu.Unlock()

	if port == nil {
		return fmt.Errorf("system is offline")
	}

	defer func() {
		w.mu.Lock()
		w.port = port
		w.mu.Unlock()
	}()

	w.mu.RLock()
	devices := make([]string, 0, len(w.activeNodes))
	for id := range w.activeNodes {
		devices = append(devices, id)
	}
	w.mu.RUnlock()

	var dbUsers []database.User
	database.DB.Find(&dbUsers)
	
	var userList []cli.GlobalUser
	for _, du := range dbUsers {
		var perms map[string]cli.GlobalPermission
		if du.Permissions != "" {
			json.Unmarshal([]byte(du.Permissions), &perms)
		}
		if perms == nil {
			perms = make(map[string]cli.GlobalPermission)
		}
		userList = append(userList, cli.GlobalUser{
			CardID:      du.CardID,
			Notes:       du.Notes,
			Permissions: perms,
		})
	}

	// Pass userList to CLI's SyncUp algorithm
	updatedUsers, err := cli.SyncUpUsersPort(port, devices, userList, "")
	if err != nil {
		return err
	}

	// Save auto-assigned addresses back to SQLite
	database.DB.Transaction(func(tx *gorm.DB) error {
		tx.Exec("DELETE FROM users")
		for _, u := range updatedUsers {
			permBytes, _ := json.Marshal(u.Permissions)
			tx.Create(&database.User{
				CardID:      u.CardID,
				Notes:       u.Notes,
				Permissions: string(permBytes),
			})
		}
		return nil
	})

	log.Println("Web Sync UP completed safely.")
	return nil
}
