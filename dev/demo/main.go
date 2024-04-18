package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	kafka "github.com/segmentio/kafka-go"

	pb "github.com/mqy/minipush/proto"
)

// TODO: write a demo client.

// The demo server mocks a business server that pushes `event`s to `kafka`.

const (
	kafkaTopic = "minipush-events"
)

var (
	kafkaEndpoints = flag.String("kafka-endpoints", "127.0.0.1:9092", "kafka endpoints, ',' delimitted.")
	tickerDuration = flag.Duration("ticker-duration", 30*time.Second, "ticker duration")
)

func main() {
	flag.Parse()

	if len(*kafkaEndpoints) == 0 {
		panic("--kafka-endpoints is required.")
	}

	endpoints := strings.Split(*kafkaEndpoints, ",")

	w := kafka.NewWriter(kafka.WriterConfig{
		Brokers:  endpoints,
		Topic:    kafkaTopic,
		Balancer: &kafka.Hash{},
		Dialer: &kafka.Dialer{
			Timeout:   10 * time.Second,
			DualStack: true,
		},
	})

	ticker := time.NewTicker(*tickerDuration)
	defer func() {
		ticker.Stop()
	}()

	var uids []int32
	for i := 0; i < 2; i++ {
		uids = append(uids, int32(i+1))
	}

	// kafka-topics.sh --bootstrap-server localhost:9092 --topic minipush-events --create
	// kafka-topics.sh --bootstrap-server localhost:9092 --topic minipush-events --delete

	var i int = 0
	for range ticker.C {
		//offset := i % len(uids)
		evt := &pb.E2EMsg{
			Type:  "a/b/c",
			From:  3,
			To:    uids,
			Body:  "hello",
			Props: map[string]string{"mime": "text/plain"},
		}

		value, err := json.Marshal(&evt)
		if err != nil {
			panic(err)
		}

		msg := kafka.Message{
			Key:   []byte(fmt.Sprintf("%d", i)),
			Value: value,
		}
		if err := w.WriteMessages(context.Background(), msg); err != nil {
			panic(err)
		}

		i++
	}
}
