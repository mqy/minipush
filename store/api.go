package store

import (
	"context"
	"time"

	pb "github.com/mqy/minipush/proto"
)

// Event holds data from kafka, to be split and push to users.
type Event struct {
	Offset     int64     `json:"-"`                 // kafka offset
	CreateTime time.Time `json:"-"`                 // unix timestamp from kafka
	To         []int32   `json:"uids,omitempty"`    // to user ids extracted from payload `to`.
	Payload    string    `json:"payload,omitempty"` // json format message.
}

type IEventStore interface {
	// GetEvents gets events in given seq range, order by seq DESC.
	GetEvents(ctx context.Context, uid int32, req *pb.GetEventsReq) (*pb.GetEventsResp, error)

	// GetReadStates gets read states in given seq range order by seq DESC.
	GetReadStates(ctx context.Context, uid, ttlDays int32, req *pb.GetReadStatesReq) (*pb.GetReadStatesResp, error)

	// SetRead sets the event as read.
	SetRead(ctx context.Context, uid, seq int32) (*pb.SetReadResp, error)

	// Stats gets max seq and counts unread events.
	Stats(ctx context.Context, uid, ttlDays int32, req *pb.StatsReq) (*pb.StatsResp, error)

	// Save splits and inserts given event into rows for each element of `e.Uids`.
	// Return map from uid to seq.
	Save(ctx context.Context, e *Event) (map[int32]int32, error)

	// DeleteOutdatedEvents deletes outdated events.
	DeleteOutdatedEvents(ctx context.Context, ttlDays int32) (int32, error)

	IsDupKeyError(err error) bool
}
