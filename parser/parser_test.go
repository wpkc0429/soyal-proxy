package parser

import (
	"testing"
	"time"
)

func TestVerifyChecksum(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected bool
	}{
		{
			name:     "Valid short frame",
			// 7E 05 01 12 00
			// XOR = FF ^ 01 ^ 12 ^ 00 = FE ^ 12 = EC
			// SUM = 01 + 12 + 00 + EC = 13 + EC = FF
			data:     []byte{0x7E, 0x05, 0x01, 0x12, 0x00, 0xEC, 0xFF},
			expected: true,
		},
		{
			name: "Invalid checksum sum",
			data: []byte{0x7E, 0x05, 0x01, 0x12, 0x00, 0xEC, 0xFE},
			expected: false,
		},
		{
			name: "Invalid checksum xor",
			data: []byte{0x7E, 0x05, 0x01, 0x12, 0x00, 0xEB, 0xFF},
			expected: false,
		},
		{
			name: "Too short",
			data: []byte{0x7E, 0x05, 0x01, 0x12},
			expected: false,
		},
		{
			name: "Length mismatch",
			// length is 0x06 but only 7 bytes provided
			data: []byte{0x7E, 0x06, 0x01, 0x12, 0x00, 0xEC, 0xFF},
			expected: false,
		},
		{
			name: "Invalid header",
			data: []byte{0x7F, 0x05, 0x01, 0x12, 0x00, 0xEC, 0xFF},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VerifyChecksum(tt.data)
			if result != tt.expected {
				t.Errorf("VerifyChecksum() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetEventDescription(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{11, "Normal Access by tag"},
		{3, "Invalid card"},
		{17, "Alarm event"},
		{99, "Event Code: 99"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := GetEventDescription(tt.code); got != tt.want {
				t.Errorf("GetEventDescription() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseEventLog_Valid(t *testing.T) {
	// Construct a dummy 27H event log structure
	data := make([]byte, 27)
	data[0] = 0x7E 
	data[1] = 0x19 // Length 25 bytes
	data[2] = 0x00 // Host DID
	data[3] = 0x27 // CMD
	data[4] = 0x01 // Source DID
	data[5] = 45   // Sec
	data[6] = 30   // Min
	data[7] = 12   // Hr
	data[8] = 3    // Wk
	data[9] = 23   // Day
	data[10] = 3   // Mon (March)
	data[11] = 26  // Yr (2026)
	
	// Example event code 11 (Normal Access)
	data[15] = 11 

	// Site code = 100 (0x0064)
	data[19] = 0x00
	data[20] = 0x64
	// Card code = 12345 (0x3039)
	data[23] = 0x30
	data[24] = 0x39

	event, err := ParseEventLog(data, "1", "Test Device")
	if err != nil {
		t.Fatalf("ParseEventLog returned unexpected error: %v", err)
	}

	if event.DeviceName != "Test Device" {
		t.Errorf("Expected DeviceName 'Test Device', got %s", event.DeviceName)
	}
	
	expectedCardID := "00100:12345"
	if event.CardID != expectedCardID {
		t.Errorf("Expected CardID '%s', got %s", expectedCardID, event.CardID)
	}

	if event.EventCode != 11 {
		t.Errorf("Expected EventCode 11, got %d", event.EventCode)
	}

	expectedTime := time.Date(2026, 3, 23, 12, 30, 45, 0, time.Local)
	if !event.Time.Equal(expectedTime) {
		t.Errorf("Expected Time %v, got %v", expectedTime, event.Time)
	}
}

func TestParseEventLog_UserAddrFallback(t *testing.T) {
	// Construct an event structure that has 0 tag ID but valid User Address
	data := make([]byte, 27)
	data[3] = 0x27 // CMD

	// Zero tag ID
	data[19] = 0
	data[20] = 0
	data[23] = 0
	data[24] = 0

	// User Address = 50 (0x0032)
	data[13] = 0x00
	data[14] = 0x32

	event, err := ParseEventLog(data, "1", "Test Device")
	if err != nil {
		t.Fatalf("ParseEventLog returned error: %v", err)
	}

	expectedCardID := "User-00050"
	if event.CardID != expectedCardID {
		t.Errorf("Expected fallback CardID '%s', got %s", expectedCardID, event.CardID)
	}
}

func TestParseEventLog_Invalid(t *testing.T) {
	// Invalid CMD
	data := make([]byte, 27)
	data[3] = 0x12 
	
	_, err := ParseEventLog(data, "1", "Device")
	if err == nil {
		t.Error("Expected error for invalid CMD, got nil")
	}

	// Too short
	shortData := make([]byte, 10)
	shortData[3] = 0x27
	_, err = ParseEventLog(shortData, "1", "Device")
	if err == nil {
		t.Error("Expected error for too short data, got nil")
	}
}
