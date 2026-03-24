package serialworker

import (
	"encoding/json"
	"fmt"
	"log"

	"gorm.io/gorm"
	"soyal-proxy/cli"
	"soyal-proxy/database"
)

func unionInts(a, b []int) []int {
	m := make(map[int]bool)
	for _, v := range a { m[v] = true }
	for _, v := range b { m[v] = true }
	var res []int
	for k := range m { res = append(res, k) }
	return res
}

func highestMode(a, b string) string {
	val := map[string]int{"": 0, "card": 1, "card_or_pin": 2, "card_and_pin": 3}
	if val[a] > val[b] {
		return a
	}
	return b
}

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
		for _, u := range globalUsers {
			permBytes, _ := json.Marshal(u.Permissions)
			res := tx.Model(&database.User{}).Where("card_id = ?", u.CardID).Updates(map[string]interface{}{
				"notes":       u.Notes,
				"permissions": string(permBytes),
			})
			if res.RowsAffected == 0 { // New user from hardware
				tx.Create(&database.User{
					CardID:      u.CardID,
					Notes:       u.Notes,
					Permissions: string(permBytes),
				})
			}
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

	// Fetch Groups for RBAC Merge
	var dbGroups []database.Group
	database.DB.Find(&dbGroups)
	groupMap := make(map[uint]string) // GroupID to JSON permissions string
	for _, g := range dbGroups {
		groupMap[g.ID] = g.Permissions
	}
	
	var userList []cli.GlobalUser
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

		// 權限繼承 (Multi-Group Union Merge)
		for _, gid := range gids {
			if groupPermsJSON, ok := groupMap[gid]; ok && groupPermsJSON != "" {
				var groupPerms map[string]cli.GlobalPermission
				json.Unmarshal([]byte(groupPermsJSON), &groupPerms)
				
				for nodeStr, gp := range groupPerms {
					if up, exists := perms[nodeStr]; !exists {
						perms[nodeStr] = gp
					} else {
						// Merge logic!
						if len(up.Doors) > 0 && len(gp.Doors) > 0 {
							up.Doors = unionInts(up.Doors, gp.Doors)
						} else {
							up.Doors = []int{} // One of them allows ALL doors
						}
						if len(up.Floors) > 0 && len(gp.Floors) > 0 {
							up.Floors = unionInts(up.Floors, gp.Floors)
						} else {
							up.Floors = []int{}
						}
						up.Mode = highestMode(up.Mode, gp.Mode)
						if up.Expiry == "" {
							up.Expiry = gp.Expiry
						}
						if up.Zone == nil && gp.Zone != nil {
							up.Zone = gp.Zone
						}
						perms[nodeStr] = up
					}
				}
			}
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

	// Save auto-assigned addresses back to SQLite safely without losing GroupIDs
	database.DB.Transaction(func(tx *gorm.DB) error {
		for _, u := range updatedUsers {
			permBytes, _ := json.Marshal(u.Permissions)
			tx.Model(&database.User{}).Where("card_id = ?", u.CardID).Update("permissions", string(permBytes))
		}
		return nil
	})

	log.Println("Web Sync UP completed safely.")
	return nil
}
