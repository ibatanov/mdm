package infra

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

type KafkaChecker struct {
	brokers []string
	timeout time.Duration
}

func NewKafkaChecker(brokers []string) *KafkaChecker {
	return &KafkaChecker{
		brokers: brokers,
		timeout: 2 * time.Second,
	}
}

func (k *KafkaChecker) Ping(ctx context.Context) error {
	if len(k.brokers) == 0 {
		return errors.New("no kafka brokers configured")
	}

	dialer := net.Dialer{Timeout: k.timeout}
	var lastErr error
	for _, broker := range k.brokers {
		conn, err := dialer.DialContext(ctx, "tcp", broker)
		if err != nil {
			lastErr = err
			continue
		}
		_ = conn.Close()
		return nil
	}

	return fmt.Errorf("all kafka brokers are unreachable: %w", lastErr)
}
