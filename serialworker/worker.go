package serialworker

import (
	"fmt"
	"log"
	"soyal-proxy/config"
	"soyal-proxy/parser"
	"soyal-proxy/publisher"
	"sync"
	"time"

	"go.bug.st/serial"
)

type Worker struct {
	port        serial.Port
	cfg         *config.Config
	publisher   *publisher.RedisPublisher
	activeNodes map[string]string
	CommandChan chan publisher.ControlMessage // Channel for priority writes
	mu          sync.RWMutex
}

func NewWorker(cfg *config.Config, pub *publisher.RedisPublisher) *Worker {
	w := &Worker{
		cfg:         cfg,
		publisher:   pub,
		activeNodes: make(map[string]string),
		CommandChan: make(chan publisher.ControlMessage, 20),
	}

	mode := &serial.Mode{
		BaudRate: cfg.BaudRate,
	}
	port, err := serial.Open(cfg.SerialPort, mode)
	if err != nil {
		log.Printf("WARNING: Failed to open serial port %s: %v", cfg.SerialPort, err)
		log.Println("System is running in OFFLINE mode. Web UI is still accessible.")
	} else {
		port.SetReadTimeout(200 * time.Millisecond)
		w.port = port
	}

	return w
}

func (w *Worker) IsOnline() bool {
	return w.port != nil
}

func (w *Worker) Start() {
	// Pre-load devices from config
	w.mu.Lock()
	for id, name := range w.cfg.Devices {
		w.activeNodes[id] = name
	}
	w.mu.Unlock()

	if w.port == nil {
		// Offline mode: Drain commands to avoid blocking other systems
		go func() {
			for cmd := range w.CommandChan {
				log.Printf("OFFLINE: Discarded remote command '%s' for Node %d", cmd.Action, cmd.NodeID)
			}
		}()
		return
	}

	go w.readLoop()
	w.autoDiscover()
	go w.pollLoop()
}

func (w *Worker) calculateChecksum(cmd []byte) []byte {
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

func (w *Worker) autoDiscover() {
	log.Println("Starting auto-discovery of devices (Scanning Nodes 1-16 for models)...")
	for i := byte(1); i <= 16; i++ {
		// Send 12H 00H (Read controller's parameters) to get Device Model
		cmd := []byte{0x7E, 0x05, i, 0x12, 0x00}
		cmd = w.calculateChecksum(cmd)
		w.port.Write(cmd)
		// Small delay to allow response in half-duplex RS485
		time.Sleep(50 * time.Millisecond)
	}
	// Wait extra time for the last responses to be parsed by readLoop
	time.Sleep(500 * time.Millisecond)

	w.mu.RLock()
	log.Printf("Auto-discovery finished. Total registered devices: %d", len(w.activeNodes))
	w.mu.RUnlock()
}

func (w *Worker) pollEventLog(nodeIDStr string) {
	var nodeID byte
	fmt.Sscanf(nodeIDStr, "%d", &nodeID)
	// 7E 04 DID 25 XOR SUM
	cmd := []byte{0x7E, 0x04, nodeID, 0x25}
	cmd = w.calculateChecksum(cmd)
	w.port.Write(cmd)
}

func (w *Worker) deleteOldestEventLog(nodeID byte) {
	// 7E 04 DID 37 XOR SUM
	cmd := []byte{0x7E, 0x04, nodeID, 0x37}
	cmd = w.calculateChecksum(cmd)
	w.port.Write(cmd)
}

func (w *Worker) handleControlCommand(cmd publisher.ControlMessage) {
	nodeID := byte(cmd.NodeID)
	// Example actions. Default to Door Open (82).
	var subCmd byte
	switch cmd.Action {
	case "open", "open_door":
		subCmd = 0x82 // Output 2 (Door Relay) Latch/Timer
	case "close_door":
		subCmd = 0x83 // Output 2 OFF
	case "pulse_door", "garage_toggle":
		subCmd = 0x84 // Output 2 Pulse
	case "alarm_on":
		subCmd = 0x80 // Output 1 (Alarm Relay) ON
	case "alarm_off":
		subCmd = 0x81 // Output 1 OFF
	case "pulse_alarm", "garage_stop":
		subCmd = 0x87 // Output 1 Pulse
	default:
		log.Printf("Unknown remote action: %s", cmd.Action)
		return
	}

	log.Printf("Executing priority command '%s' (21H %X) on Node %d via Redis", cmd.Action, subCmd, nodeID)
	pkt := []byte{0x7E, 0x05, nodeID, 0x21, subCmd, 0x00}
	pkt = w.calculateChecksum(pkt)
	w.port.Write(pkt)
	
	// Wait a bit for the device to ACK before resuming normal polling
	time.Sleep(100 * time.Millisecond)
}

func (w *Worker) pollLoop() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	idx := 0
	for {
		select {
		case cmd := <-w.CommandChan:
			// Priority execution of Redis remote control commands
			w.handleControlCommand(cmd)

		case <-ticker.C:
			// Normal background polling
			w.mu.RLock()
			nodeIDs := make([]string, 0, len(w.activeNodes))
			for id := range w.activeNodes {
				nodeIDs = append(nodeIDs, id)
			}
			w.mu.RUnlock()

			if len(nodeIDs) == 0 {
				continue // No devices active
			}

			if idx >= len(nodeIDs) {
				idx = 0
			}
			node := nodeIDs[idx]
			w.pollEventLog(node)
			idx++

			// Wait briefly after polling Event Log to avoid RS-485 contention
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func (w *Worker) readLoop() {
	buf := make([]byte, 1024)
	var frame []byte

	for {
		n, err := w.port.Read(buf)
		if err != nil {
			log.Printf("Serial read error: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}
		if n == 0 {
			continue
		}

		frame = append(frame, buf[:n]...)

		for len(frame) >= 6 {
			idx := -1
			for i, b := range frame {
				if b == 0x7E { // Standard Short frame starts with 7E
					idx = i
					break
				}
			}
			if idx == -1 {
				frame = nil
				break
			}
			frame = frame[idx:]

			if len(frame) < 6 {
				break
			}

			length := int(frame[1])
			if len(frame) < length+2 {
				break
			}

			pkt := frame[:length+2]
			if parser.VerifyChecksum(pkt) {
				dataFnCode := pkt[3] // Function Code of echo
				var nodeID string
				var sourceDID byte

				if dataFnCode == 0x27 || dataFnCode == 0x09 || dataFnCode == 0x03 {
					// 03: Data Echo, 09: Echo Reader status, 27: Event Log Echo
					if len(pkt) > 4 {
						sourceDID = pkt[4]
						nodeID = fmt.Sprintf("%d", sourceDID)
					}
				}

				if nodeID != "" {
					w.mu.RLock()
					devName := w.activeNodes[nodeID]
					w.mu.RUnlock()

					isNew := false
					if devName == "" {
						devName = fmt.Sprintf("Auto Node %s", nodeID)
						isNew = true
					}

					// If response is 03H (Data Echo for 12H), byte 5 has Controller Type
					if dataFnCode == 0x03 && len(pkt) > 5 {
						ctrlType := pkt[5]
						var model string
						switch ctrlType {
						case 0xC0:
							model = "AR-881E"
						case 0xC1:
							model = "AR-725Ev2"
						case 0xC2:
							model = "AR-829Ev5"
						case 0xC3:
							model = "AR-821EFv5"
						case 0xC4:
							model = "AR-727Ev5"
						case 0xC5:
							model = "AR-721Ev2"
						default:
							model = fmt.Sprintf("SOYAL Device(%X)", ctrlType)
						}

						// Update name if we just discovered it or it still has the default "Auto Node" name
						if isNew || devName == fmt.Sprintf("Auto Node %s", nodeID) {
							devName = fmt.Sprintf("%s (Node %s)", model, nodeID)
							isNew = true
						}
					}

					// Register new device
					if isNew {
						w.mu.Lock()
						w.activeNodes[nodeID] = devName
						w.mu.Unlock()
						log.Printf("Auto-discovered active device: %s", devName)
					}

					// Process event log
					if dataFnCode == 0x27 {
						evt, err := parser.ParseEventLog(pkt, nodeID, devName)
						if err == nil {
							log.Printf("Event received: %+v\n", evt)
							w.publisher.Publish(evt)
							
							// Acknowledge by deleting event log
							w.deleteOldestEventLog(sourceDID)
						} else {
							log.Printf("Failed to parse event log: %v", err)
						}
					}
				}

				frame = frame[length+2:]
			} else {
				frame = frame[1:] // Invalid checksum, skip this 7E and look for next
			}
		}
	}
}
