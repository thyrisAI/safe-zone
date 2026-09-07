package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

const (
	RedisPolicyPrefix      = "tsz:policy:"
	RedisActivationChannel = RedisPolicyPrefix + "activated"
)

type ActivationEvent struct {
	PolicyID string `json:"policy_id"`
	Version  int    `json:"version"`
}

type ActivationPublisher interface {
	PublishActivation(ctx context.Context, event ActivationEvent) error
}

// RedisActivationPublisher reuses an injected application Redis client. It
// never creates or owns a Redis connection.
type RedisActivationPublisher struct {
	client *redis.Client
}

func NewRedisActivationPublisher(client *redis.Client) (*RedisActivationPublisher, error) {
	if client == nil {
		return nil, errors.New("existing Redis client is required")
	}
	return &RedisActivationPublisher{client: client}, nil
}

func (p *RedisActivationPublisher) PublishActivation(ctx context.Context, event ActivationEvent) error {
	if event.PolicyID == "" || event.Version <= 0 {
		return fmt.Errorf("invalid policy activation event: %+v", event)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal policy activation event: %w", err)
	}
	if err := p.client.Publish(ctx, RedisActivationChannel, payload).Err(); err != nil {
		return fmt.Errorf("publish %s: %w", RedisActivationChannel, err)
	}
	return nil
}
