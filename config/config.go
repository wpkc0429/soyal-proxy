package config

import (
	"encoding/json"
	"os"
)

type Config struct {
	SerialPort string            `json:"serial_port"`
	BaudRate   int               `json:"baud_rate"`
	RedisHost  string            `json:"redis_host"`
	RedisPass  string            `json:"redis_pass"`
	RedisTopic string            `json:"redis_topic"`
	Devices    map[string]string `json:"devices"` // NodeID as string -> Device Name
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
