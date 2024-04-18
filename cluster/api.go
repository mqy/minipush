package cluster

import (
	"context"

	"github.com/segmentio/kafka-go"

	pb "github.com/mqy/minipush/proto"
)

type IKafkaReader interface {
	FetchMessage(context.Context) (kafka.Message, error)
	CommitMessages(context.Context, ...kafka.Message) error
	Close() error
}

type IKafkaWriter interface {
	WriteMessages(context.Context, ...kafka.Message) error
	Close() error
}

type ICluster interface {
	Run(ctx context.Context, stopNotifyCh chan<- struct{})
	SaveMsg(ctx context.Context, msg *pb.E2EMsg) error
}

// IHub provides interfaces of local Hub.
type IHub interface {
	Run(context.Context, chan<- *pb.FollowerMsg, <-chan *pb.LeaderMsg, chan<- struct{})
	Kickoff(string)
	Online()
	Offline()
}
