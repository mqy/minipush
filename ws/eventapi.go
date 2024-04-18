package ws

import (
	"context"
	"fmt"

	pb "github.com/mqy/minipush/proto"
	"github.com/mqy/minipush/store"
)

const (
	MinTTLDays = 7
	MaxTTLDays = 100

	MinGetEventsLimit = 25
	MaxGetEventsLimit = 100

	ErrorCodeInvalidArguments = 3
	ErrorCodeInternal         = 13
)

// EventApi serves websocket client requests.
type EventApi struct {
	store store.IEventStore
	conf  *pb.WsConf
}

func NewApi(store store.IEventStore, conf *pb.WsConf) *EventApi {
	return &EventApi{
		store: store,
		conf:  conf,
	}
}

func (s *EventApi) Stats(ctx context.Context, uid int32, req *pb.StatsReq) (*pb.StatsResp, *pb.Error) {
	stats, err := s.store.Stats(ctx, uid, s.conf.EventTtlDays, req)
	if err != nil {
		return nil, newInternalError(&pb.ClientMsg{Stats: req}, err.Error())
	}
	stats.Conf = s.conf
	return stats, nil
}

func (s *EventApi) GetEvents(ctx context.Context, uid int32, req *pb.GetEventsReq) (*pb.GetEventsResp, *pb.Error) {
	var errs []string

	if req.FromSeq <= 0 {
		errs = append(errs, "from_seq: should be positive integer")
	}
	if req.ToSeq <= 0 {
		errs = append(errs, "to_seq: should be positive integer")
	}
	if req.FromSeq-req.ToSeq >= s.conf.GetEventsLimit {
		errs = append(errs, fmt.Sprintf("seq range: exceeds limit: %d", s.conf.GetEventsLimit))
	}

	if len(errs) > 0 {
		return nil, newInvalidArgumentError(&pb.ClientMsg{GetEvents: req}, errs...)
	}

	resp, err := s.store.GetEvents(ctx, uid, req)
	if err != nil {
		return nil, newInternalError(&pb.ClientMsg{GetEvents: req}, err.Error())
	}
	return resp, nil
}

func (s *EventApi) SetRead(ctx context.Context, uid int32, req *pb.SetReadReq) (*pb.SetReadResp, *pb.Error) {
	if req.Seq <= 0 {
		return nil, newInvalidArgumentError(&pb.ClientMsg{SetRead: req}, "seq: should be positive integer")
	}
	resp, err := s.store.SetRead(ctx, uid, req.Seq)
	if err != nil {
		return nil, newInternalError(&pb.ClientMsg{SetRead: req}, err.Error())
	}
	return resp, nil
}

func (s *EventApi) GetReadStates(ctx context.Context, uid int32, req *pb.GetReadStatesReq) (*pb.GetReadStatesResp, *pb.Error) {
	if req.HeadSeq <= 0 {
		return nil, newInvalidArgumentError(&pb.ClientMsg{GetReadStates: req}, "head_seq: should be positive integer")
	}
	resp, err := s.store.GetReadStates(ctx, uid, s.conf.EventTtlDays, req)
	if err != nil {
		return nil, newInternalError(&pb.ClientMsg{GetReadStates: req}, err.Error())
	}
	return resp, nil
}

func newInvalidArgumentError(req *pb.ClientMsg, errs ...string) *pb.Error {
	return &pb.Error{
		Code:   ErrorCodeInvalidArguments,
		Params: errs,
		Req:    req,
	}
}

func newInternalError(req *pb.ClientMsg, err string) *pb.Error {
	return &pb.Error{
		Code:   ErrorCodeInternal,
		Params: []string{err},
		Req:    req,
	}
}

func interceptError(err *pb.Error) {
	if err.Code == ErrorCodeInternal {
		err.Params = []string{"temp storage error"}
	}
}
