package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"go.bug.st/serial"
)

type GlobalPermission struct {
	UserAddr *int     `json:"user_addr,omitempty"`
	Pin      string   `json:"pin,omitempty"`    // e.g. "1234"
	Mode     string   `json:"mode,omitempty"`   // "card", "card_or_pin", "card_and_pin"
	Zone     *int     `json:"zone,omitempty"`   // 0~63
	Doors    []int    `json:"doors,omitempty"`  // [1, 2, 3... 16] (replaces group1/group2)
	Expiry   string   `json:"expiry,omitempty"` // "YYYY-MM-DD"
	Floors   []int    `json:"floors,omitempty"` // [1, 2, 3]
}

func parseBCD(b byte) int {
	return int((b>>4)*10 + (b & 0x0F))
}

func toBCD(val int) byte {
	return byte(((val / 10) << 4) | (val % 10))
}

type GlobalUser struct {
	CardID      string                      `json:"card_id"`
	Notes       string                      `json:"notes,omitempty"`
	Permissions map[string]GlobalPermission `json:"permissions"` // Node ID -> Data
}

func calculateChecksum(cmd []byte) []byte {
	var xor byte = 0xFF
	for i := 2; i < len(cmd); i++ {
		xor ^= cmd[i]
	}
	var sum uint32 = 0x00
	for i := 2; i < len(cmd); i++ {
		sum += uint32(cmd[i])
	}
	sum += uint32(xor)
	return append(cmd, xor, byte(sum&0xFF))
}

func syncDownNode(port serial.Port, nodeID byte) map[string]GlobalPermission {
	perms := make(map[string]GlobalPermission)
	fmt.Printf("Fetching whitelist from Node %d...\n", nodeID)

	for addr := 0; addr < 100; addr += 10 {
		cmd := []byte{0x7E, 0x07, nodeID, 0x87, byte(addr >> 8), byte(addr & 0xFF), 10}
		cmd = calculateChecksum(cmd)
		port.Write(cmd)

		buf := make([]byte, 1024)
		n, err := port.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		if n > 6 && buf[0] == 0x7E && buf[3] == 0x03 {
			payload := buf[5 : n-2]
			recordSize := 24
			if len(payload)%26 == 0 {
				recordSize = 26
			}

			// Also fetch floor data via 2FH (assuming 8 bytes per user for 64-floor mode)
			floorIndex := addr * 8
			floorRecords := 10 * 8
			cmd2F := []byte{0x7E, 0x09, nodeID, 0x2F,
				byte(floorIndex >> 24), byte(floorIndex >> 16), byte(floorIndex >> 8), byte(floorIndex),
				byte(floorRecords >> 8), byte(floorRecords),
			}
			cmd2F = calculateChecksum(cmd2F)
			port.Write(cmd2F)

			fbuf := make([]byte, 1024)
			fn, _ := port.Read(fbuf)
			floorDataMap := make(map[int]string)
			if fn > 6 && fbuf[0] == 0x7E && fbuf[3] == 0x03 {
				floorPayload := fbuf[5 : fn-2]
				// Map floors locally by their address
				for i := 0; i < 10; i++ {
					fIdx := i * 8
					if fIdx+8 <= len(floorPayload) {
						floorDataMap[addr+i] = hex.EncodeToString(floorPayload[fIdx : fIdx+8])
					}
				}
			}

			for i := 0; i < len(payload); i += recordSize {
				if i+recordSize > len(payload) {
					break
				}
				recBytes := payload[i : i+recordSize]

				isEmpty := true
				for _, b := range recBytes {
					if b != 0x00 && b != 0xFF {
						isEmpty = false
						break
					}
				}

				if !isEmpty && len(recBytes) >= 10 {
					siteCode := (uint32(recBytes[6]) << 8) | uint32(recBytes[7])
					cardCode := (uint32(recBytes[8]) << 8) | uint32(recBytes[9])
					cardStr := fmt.Sprintf("%05d:%05d", siteCode, cardCode)

					userAddr := addr + (i / recordSize)
					
					var floors []int
					floorHexStr := floorDataMap[userAddr]
					if floorHexStr != "" {
						fBytes, _ := hex.DecodeString(floorHexStr)
						for byteIdx, b := range fBytes {
							for bit := 0; bit < 8; bit++ {
								if (b & (1 << bit)) != 0 {
									floors = append(floors, byteIdx*8+bit+1)
								}
							}
						}
					}

					pinVal := uint32(recBytes[10])<<24 | uint32(recBytes[11])<<16 | uint32(recBytes[12])<<8 | uint32(recBytes[13])
					var pinStr string
					if pinVal != 0 && pinVal != 0xFFFFFFFF {
						pinStr = fmt.Sprintf("%d", pinVal)
					}

					modeByte := recBytes[14]
					var modeStr string
					if (modeByte & 0xC0) == 0x40 {
						modeStr = "card"
					} else if (modeByte & 0xC0) == 0x80 {
						modeStr = "card_or_pin"
					} else if (modeByte & 0xC0) == 0xC0 {
						modeStr = "card_and_pin"
					}

					zoneVal := int(recBytes[15])
					var zonePtr *int
					if zoneVal != 0 {
						zonePtr = &zoneVal
					}

					var doors []int
					b16, b17 := recBytes[16], recBytes[17]
					if b16 != 0xFF || b17 != 0xFF {
						for bit := 0; bit < 8; bit++ {
							if (b16 & (1 << bit)) != 0 {
								doors = append(doors, bit+1)
							}
							if (b17 & (1 << bit)) != 0 {
								doors = append(doors, bit+9)
							}
						}
					}

					expiry := fmt.Sprintf("20%02d-%02d-%02d", parseBCD(recBytes[18]), parseBCD(recBytes[19]), parseBCD(recBytes[20]))

					perms[cardStr] = GlobalPermission{
						UserAddr: &userAddr,
						Pin:      pinStr,
						Mode:     modeStr,
						Zone:     zonePtr,
						Doors:    doors,
						Expiry:   expiry,
						Floors:   floors,
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return perms
}

func SyncDownAll(serialPort string, baudRate int, devices map[string]string) error {
	fmt.Printf("Starting Global Sync DOWN on %s...\n", serialPort)

	mode := &serial.Mode{BaudRate: baudRate}
	port, err := serial.Open(serialPort, mode)
	if err != nil {
		return fmt.Errorf("failed to open serial port: %v", err)
	}
	defer port.Close()
	port.SetReadTimeout(2 * time.Second)

	globalUsers := make(map[string]*GlobalUser) // Keyed by CardID

	for nodeStr := range devices {
		var nodeID byte
		fmt.Sscanf(nodeStr, "%d", &nodeID)

		nodePerms := syncDownNode(port, nodeID)
		
		for cardID, perm := range nodePerms {
			if globalUsers[cardID] == nil {
				globalUsers[cardID] = &GlobalUser{
					CardID:      cardID,
					Permissions: make(map[string]GlobalPermission),
				}
			}
			globalUsers[cardID].Permissions[nodeStr] = perm
		}
	}

	// Convert map to slice for JSON
	var userList []GlobalUser
	for _, u := range globalUsers {
		userList = append(userList, *u)
	}

	filename := "global_users.json"
	data, _ := json.MarshalIndent(userList, "", "  ")
	os.WriteFile(filename, data, 0644)
	fmt.Printf("Global Sync DOWN completed. Saved %d users to %s\n", len(userList), filename)
	return nil
}

func SyncUpAll(serialPort string, baudRate int, devices map[string]string) error {
	filename := "global_users.json"
	fmt.Printf("Starting Global Sync UP from %s...\n", filename)

	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read %s: %v", filename, err)
	}

	var userList []GlobalUser
	if err := json.Unmarshal(data, &userList); err != nil {
		return fmt.Errorf("invalid json: %v", err)
	}

	// 1. First pass: Build a map of used addresses for each node
	usedAddrs := make(map[string]map[int]bool)
	for i := range userList {
		u := &userList[i]
		for nodeStr, perm := range u.Permissions {
			if usedAddrs[nodeStr] == nil {
				usedAddrs[nodeStr] = make(map[int]bool)
			}
			if perm.UserAddr != nil {
				usedAddrs[nodeStr][*perm.UserAddr] = true
			}
		}
	}

	// 2. Second pass: Auto-assign available addresses starting from 1 for omitted addresses
	rewritten := false
	for i := range userList {
		u := &userList[i]
		for nodeStr, perm := range u.Permissions {
			if perm.UserAddr == nil {
				addr := 1
				for usedAddrs[nodeStr][addr] {
					addr++
				}
				usedAddrs[nodeStr][addr] = true
				
				perm.UserAddr = &addr
				u.Permissions[nodeStr] = perm // Update the map value
				rewritten = true
				fmt.Printf("Auto-assigned UserAddr %d for Card %s on Node %s\n", addr, u.CardID, nodeStr)
			}
		}
	}

	// 3. Save auto-assigned addresses back to JSON so user knows their slots
	if rewritten {
		updatedData, _ := json.MarshalIndent(userList, "", "  ")
		os.WriteFile(filename, updatedData, 0644)
		fmt.Println("Saved newly assigned addresses to global_users.json")
	}

	mode := &serial.Mode{BaudRate: baudRate}
	port, err := serial.Open(serialPort, mode)
	if err != nil {
		return fmt.Errorf("failed to open serial port: %v", err)
	}
	defer port.Close()
	port.SetReadTimeout(2 * time.Second)

	// Group operations by Node to minimize switching
	for nodeStr := range devices {
		var nodeID byte
		fmt.Sscanf(nodeStr, "%d", &nodeID)
		fmt.Printf("Writing to Node %s...\n", nodeStr)

		wroteAnything := false
		for _, u := range userList {
			perm, exists := u.Permissions[nodeStr]
			if !exists {
				continue
			}

			// Generate payload based on JSON fields
			recBytes := make([]byte, 26)
			
			// Mode string to bytes
			switch perm.Mode {
			case "card":
				recBytes[14] = 0x40
			case "card_or_pin":
				recBytes[14] = 0x80
			case "card_and_pin":
				recBytes[14] = 0xC0
			default:
				recBytes[14] = 0x40 // Default: Card Only
			}

			if perm.Zone != nil {
				recBytes[15] = byte(*perm.Zone)
			} else {
				recBytes[15] = 0x00 // Default Zone 0
			}

			// Doors array mapping
			if perm.Doors == nil {
				recBytes[16] = 0xFF // All doors
				recBytes[17] = 0xFF // All doors
			} else {
				recBytes[16] = 0x00
				recBytes[17] = 0x00
				for _, d := range perm.Doors {
					if d >= 1 && d <= 8 {
						recBytes[16] |= (1 << (d - 1))
					} else if d >= 9 && d <= 16 {
						recBytes[17] |= (1 << (d - 9))
					}
				}
			}

			// Expiry mapping to BCD
			if perm.Expiry != "" {
				var y, m, d int
				fmt.Sscanf(perm.Expiry, "%d-%d-%d", &y, &m, &d)
				if y > 2000 {
					y -= 2000
				}
				recBytes[18] = toBCD(y)
				recBytes[19] = toBCD(m)
				recBytes[20] = toBCD(d)
			} else {
				recBytes[18] = 0x99 // 2099
				recBytes[19] = 0x12 // Dec
				recBytes[20] = 0x31 // 31
			}

			// PIN mapping
			if perm.Pin != "" {
				var pinVal uint32
				fmt.Sscanf(perm.Pin, "%d", &pinVal)
				recBytes[10] = byte((pinVal >> 24) & 0xFF)
				recBytes[11] = byte((pinVal >> 16) & 0xFF)
				recBytes[12] = byte((pinVal >> 8) & 0xFF)
				recBytes[13] = byte(pinVal & 0xFF)
			}

			actualAddr := *perm.UserAddr
			recBytes[0] = byte(actualAddr & 0xFF)
			recBytes[1] = byte((actualAddr >> 8) & 0xFF)

			var siteCode, cardCode uint32
			_, err = fmt.Sscanf(u.CardID, "%d:%d", &siteCode, &cardCode)
			if err == nil {
				recBytes[6] = byte((siteCode >> 8) & 0xFF)
				recBytes[7] = byte(siteCode & 0xFF)
				recBytes[8] = byte((cardCode >> 8) & 0xFF)
				recBytes[9] = byte(cardCode & 0xFF)
			}

			length := byte(2 + 1 + 1 + 26)
			cmd := []byte{0x7E, length, nodeID, 0x83, 1}
			cmd = append(cmd, recBytes...)
			cmd = calculateChecksum(cmd)

			port.Write(cmd)
			wroteAnything = true

			buf := make([]byte, 128)
			port.Read(buf)
			time.Sleep(100 * time.Millisecond)

			// Update Floor Data via 2FH using integer array
			if len(perm.Floors) > 0 {
				fBytes := make([]byte, 8) // Assume 64-floor array
				for _, f := range perm.Floors {
					if f >= 1 && f <= 64 {
						idx := (f - 1) / 8
						bit := (f - 1) % 8
						fBytes[idx] |= (1 << bit)
					}
				}

				floorIndex := actualAddr * len(fBytes)
				floorRecords := len(fBytes)

				cmd2F := []byte{0x7E, byte(8 + len(fBytes)), nodeID, 0x2F,
					byte(floorIndex >> 24), byte(floorIndex >> 16), byte(floorIndex >> 8), byte(floorIndex),
					byte(floorRecords >> 8), byte(floorRecords),
				}
				cmd2F = append(cmd2F, fBytes...)
				cmd2F = calculateChecksum(cmd2F)
				port.Write(cmd2F)
				port.Read(buf) // flush echo
				time.Sleep(50 * time.Millisecond)
			}
		}
		
		if !wroteAnything {
			fmt.Println("  No cards assigned to this node. Skipping.")
		}
	}

	fmt.Println("Global Sync UP completed successfully.")
	return nil
}
