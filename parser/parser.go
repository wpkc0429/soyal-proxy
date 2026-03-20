package parser

import (
	"fmt"
	"time"
)

type AccessEvent struct {
	DeviceName string    `json:"device_name"`
	CardID     string    `json:"card_id"`
	Time       time.Time `json:"time"`
	EventCode  int       `json:"event_code"`
	EventDesc  string    `json:"event_desc"`
}

// VerifyChecksum verifies SOYAL 7E XOR and SUM checksums.
func VerifyChecksum(data []byte) bool {
	if len(data) < 6 || data[0] != 0x7E {
		return false
	}
	length := int(data[1])
	if len(data) != length+2 {
		return false
	}

	var xor byte = 0xFF
	for i := 2; i < len(data)-2; i++ {
		xor ^= data[i]
	}
	if data[len(data)-2] != xor {
		return false
	}

	var sum uint32 = 0x00
	for i := 2; i < len(data)-1; i++ { // sum from DID to XOR
		sum += uint32(data[i])
	}
	if byte(sum&0xFF) != data[len(data)-1] {
		return false
	}
	return true
}

func GetEventDescription(code int) string {
	switch code {
	case 11:
		return "Normal Access by tag"
	case 3:
		return "Invalid card"
	case 2:
		return "Keypad Locked"
	case 4:
		return "Time Zone error"
	case 6:
		return "Expiry Date"
	case 1:
		return "Invalid user PIN"
	case 16:
		return "Egress"
	case 17:
		return "Alarm event"
	default:
		return fmt.Sprintf("Event Code: %d", code)
	}
}

// ParseEventLog parses a 27H event log structure from device (echoed by 25H command).
func ParseEventLog(data []byte, nodeID string, deviceName string) (*AccessEvent, error) {
	// Frame: 7E LEN DID CMD(27) SRC_ID Sec Min Hr Wk Day Mon Yr ...
	// Indexes:
	// 0: 7E
	// 1: Length (e.g., 21H / 33)
	// 2: DID (00 - host)
	// 3: CMD (27H)
	// 4: Source DID (Dat0)
	// 5: Sec (Dat1)
	// 6: Min (Dat2)
	// 7: Hr (Dat3)
	// 8: Wk (Dat4)
	// 9: Day (Dat5)
	// 10: Mon (Dat6)
	// 11: Yr (Dat7)
	// ...
	// 15: Sub Code (Event function code, Dat11)
	// 19: Tag ID (Dat15)
	// 20: Tag ID (Dat16)
	// 23: Tag ID (Dat19)
	// 24: Tag ID (Dat20)

	if len(data) < 28 || data[3] != 0x27 {
		return nil, fmt.Errorf("invalid event log frame")
	}

	yr := int(data[11]) + 2000
	mon := time.Month(data[10])
	day := int(data[9])
	hr := int(data[7])
	min := int(data[6])
	sec := int(data[5])
	t := time.Date(yr, mon, day, hr, min, sec, 0, time.Local)

	eventCode := int(data[15])
	eventDesc := GetEventDescription(eventCode)

	// Extract Tag ID
	// Data 15, 16 are Site Code (bits 31~16 of 32-bit tag). mapped to pkt[19], pkt[20]
	// Data 19, 20 are Card Code (bits 15~00 of 32-bit tag). mapped to pkt[23], pkt[24]
	cardStr := ""
	if len(data) > 24 {
		siteCode := uint32(data[19])<<8 | uint32(data[20])
		cardCode := uint32(data[23])<<8 | uint32(data[24])

		if siteCode == 0 && cardCode == 0 {
			// fallback to user address (Data 9 & Data 10 -> pkt[13], pkt[14])
			userAddr := uint32(data[13])<<8 | uint32(data[14])
			if userAddr != 0 {
				cardStr = fmt.Sprintf("User-%05d", userAddr)
			} else {
				cardStr = "N/A"
			}
		} else {
			cardStr = fmt.Sprintf("%05d:%05d", int(siteCode), int(cardCode))
		}
	}

	return &AccessEvent{
		DeviceName: deviceName,
		CardID:     cardStr,
		Time:       t,
		EventCode:  eventCode,
		EventDesc:  eventDesc,
	}, nil
}
