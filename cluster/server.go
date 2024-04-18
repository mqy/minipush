package cluster

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/mqy/minipush/proto"
)

// pushServer implements minipush gRPC server.
// see proto/cluster.proto.
type pushServer struct {
	sync.Mutex
	pb.MiniPushClusterServer
	pb.MiniPushCluster_RouteServer

	sessionQuota int32
	eventMaxSize int32
	sessionStore *SessionStore
	kafkaWriter  IKafkaWriter

	hubStreams map[string]pb.MiniPushCluster_RouteServer
	sendMsgCh  <-chan *pb.LeaderMsg
	wg         sync.WaitGroup
}

func newPushServer(sendMsgChan <-chan *pb.LeaderMsg, kafkaWriter IKafkaWriter, sessionQuota, eventMaxSize int32) *pushServer {
	return &pushServer{
		sessionQuota: sessionQuota,
		eventMaxSize: eventMaxSize,
		sessionStore: newSessionStore(),
		kafkaWriter:  kafkaWriter,
		hubStreams:   make(map[string]pb.MiniPushCluster_RouteServer),
		sendMsgCh:    sendMsgChan,
	}
}

// Route implements gRPC API `Route`.
func (s *pushServer) Route(stream pb.MiniPushCluster_RouteServer) error {
	var hubId string
	if md, ok := metadata.FromIncomingContext(stream.Context()); ok {
		if slice := md.Get("x-hub-id"); len(slice) > 0 {
			hubId = slice[0]
		}
	}
	if hubId == "" {
		panic("error x-hub-id not found from context metadata")
	}

	glog.Infof("hub %s connected, blocking", hubId)
	defer func() {
		glog.Infof("hub %s is disconnecting", hubId)
		s.removeStream(hubId)
		glog.Infof("hub %s disconnected", hubId)
	}()

	s.addStream(hubId, stream)

	for {
		msg, err := stream.Recv()
		if err != nil {
			glog.Infof("recv from hub %s error: %v", hubId, err)
			code := status.Code(err)
			// the ctx was canceled by caller on stop.
			if err == io.EOF || code == codes.Canceled || code == codes.Unavailable { // TODO: verify
				glog.Infof("recv from hub %s canceled", hubId)
				return nil
			}
			return err
		}

		if glog.V(5) {
			m := jsonpb.Marshaler{OrigName: true}
			out, _ := m.MarshalToString(msg)
			glog.V(5).Infof("recv message from hub %s: %s", hubId, out)
		}

		if v := msg.SessionOnline; v != nil {
			s.sessionStore.add(hubId, v)
			if v := s.sessionStore.getUserSessionsToKickoff(v.Uid, s.sessionQuota); len(v) > 0 {
				s.kickoff(v)
			}
		} else if v := msg.SessionOffline; v != "" {
			s.sessionStore.del(hubId, msg.SessionOffline)
		} else if v := msg.SyncSessions; len(v) > 0 {
			s.sessionStore.addMany(hubId, msg.SyncSessions)
		} else if v := msg.E2EMsg; v != nil {
			if err := saveE2eMsg(s.kafkaWriter, v, int(s.eventMaxSize)); err != nil {
				// Just ignore.
				glog.Errorf("error save im msg: %v", err)
			}
		} else if msg.ServerShutdown {
			return nil
		} else {
			panic(fmt.Sprintf("unknown message from hub %s: %#+v", hubId, msg))
		}
	}
}

func (s *pushServer) run(ctx context.Context, stopDoneNotifyC chan<- struct{}) {
	glog.Info("run(): enter")
	defer func() {
		glog.Info("run(): defer enter")
		s.wg.Wait()

		stopDoneNotifyC <- struct{}{}
		glog.Info("run(): defer exited")
	}()

	go s.cleanSessionLoop(ctx)

	for {
		select {
		case msg, ok := <-s.sendMsgCh:
			if !ok {
				return
			}
			streams := s.copyStreams()
			for hub, stream := range streams {
				s.pushToFollower(hub, stream, msg)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *pushServer) copyStreams() map[string]pb.MiniPushCluster_RouteServer {
	out := make(map[string]pb.MiniPushCluster_RouteServer)

	s.Lock()
	for k, v := range s.hubStreams {
		out[k] = v
	}
	s.Unlock()

	return out
}

func (s *pushServer) getLiveHubIds() map[string]struct{} {
	out := make(map[string]struct{})

	s.Lock()
	for k := range s.hubStreams {
		out[k] = struct{}{}
	}
	s.Unlock()

	return out
}

func (s *pushServer) copyStreamsByFollower(hubs []string) map[string]pb.MiniPushCluster_RouteServer {
	out := make(map[string]pb.MiniPushCluster_RouteServer)

	s.Lock()
	for _, hub := range hubs {
		if stream, ok := s.hubStreams[hub]; ok {
			out[hub] = stream
		}
	}
	s.Unlock()

	return out
}

func (s *pushServer) addStream(ni string, stream pb.MiniPushCluster_RouteServer) {
	s.Lock()
	s.hubStreams[ni] = stream
	s.Unlock()

	glog.Infof("addStream(): added stream, hub: %s", ni)
}

func (s *pushServer) removeStream(ni string) {
	s.Lock()
	delete(s.hubStreams, ni)
	s.Unlock()
	glog.Infof("removeStream(): removed hub stream, hub: %s", ni)
}

// TODO: don't block on a slow or problem stream, timeout?
func (s *pushServer) pushToFollower(hubId string, stream pb.MiniPushCluster_RouteServer, msg *pb.LeaderMsg) bool {
	if glog.V(5) {
		m := jsonpb.Marshaler{OrigName: true}
		out, _ := m.MarshalToString(msg)
		glog.V(5).Infof("pushToFollower(), hub: %s, msg: %s", hubId, out)
	}

	if err := stream.Send(msg); err != nil {
		glog.Errorf("pushToFollower: error send msg %+v to hub %s, err: %v", msg, hubId, err)
		code := status.Code(err)
		if err == io.EOF || code == codes.Canceled || code == codes.Unavailable {
			s.removeStream(hubId)
		}
		return false
	}
	return true
}

func (s *pushServer) cleanSessionLoop(ctx context.Context) {
	glog.Infof("cleanSessionLoop: running, blocking")
	s.wg.Add(1)
	ticker := time.NewTicker(time.Minute)
	defer func() {
		ticker.Stop()
		glog.Infof("cleanSessionLoop: exited")
		s.wg.Done()
	}()

	for {
		select {
		case <-ctx.Done():
			s.sessionStore.empty()
			return
		case <-ticker.C:
			if slice := s.sessionStore.gc(s.getLiveHubIds(), s.sessionQuota); len(slice) > 0 {
				s.kickoff(slice)
			}
		}
	}
}

func (s *pushServer) kickoff(sessions map[string]*pb.Session) {
	glog.V(5).Infof("kickoff: %d sessions", len(sessions))

	targets := make(map[string][]string)
	for _, sess := range sessions {
		targets[sess.HubId] = append(targets[sess.HubId], sess.Sid)
	}

	var hubs []string
	for k := range targets {
		hubs = append(hubs, k)
	}

	// len(streams) must <= len(hubs)
	streams := s.copyStreamsByFollower(hubs)

	for hub, stream := range streams {
		if sids, ok := targets[hub]; ok {
			msg := &pb.LeaderMsg{Kickoff: sids}
			if s.pushToFollower(hub, stream, msg); ok {
				now := time.Now().Unix()
				for _, sid := range sids {
					sessions[sid].KickoffTime = now
				}
			}
		}
	}
}
