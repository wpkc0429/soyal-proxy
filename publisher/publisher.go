package publisher

import (
	"context"
	"encoding/json"
	"fmt"
	"soyal-proxy/config"
	"soyal-proxy/parser"

	"github.com/go-redis/redis/v8"
)

type RedisPublisher struct {
	client *redis.Client
	topic  string
	ctx    context.Context
}

func NewRedisPublisher(cfg *config.Config) (*RedisPublisher, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisHost,
		Password: cfg.RedisPass,
		DB:       0,
	})

	ctx := context.Background()
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %v", err)
	}

	return &RedisPublisher{
		client: rdb,
		topic:  cfg.RedisTopic,
		ctx:    ctx,
	}, nil
}

func (p *RedisPublisher) Publish(event *parser.AccessEvent) error {
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.client.Publish(p.ctx, p.topic, string(b)).Err()
}
