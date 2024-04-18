package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"

	pb "github.com/mqy/minipush/proto"
)

func saveE2eMsg(kafkaWriter IKafkaWriter, msg *pb.E2EMsg, limit int) error {
	value, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("error marshal im: %q, err: %v", msg, err)
	}
	if len(value) > limit {
		return fmt.Errorf("storage: msg exceeds max limit: %d bytes", limit)
	}

	km := kafka.Message{
		Value: value,
	}

	ctx2, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := kafkaWriter.WriteMessages(ctx2, km); err != nil {
		return fmt.Errorf("error write to kafka: %s", err)
	}
	return nil
}
