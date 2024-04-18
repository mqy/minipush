package cluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/golang/glog"
	kafka "github.com/segmentio/kafka-go"

	pb "github.com/mqy/minipush/proto"
)

// Standalone is a standalone server, with out running leader and follower.
// It is equivalent to one node cluster with gRPC client and server
type Standalone struct {
	ICluster
	sync.RWMutex
	sessions map[string]*pb.Session

	conf        *ClusterCfg
	httpServer  *http.Server
	storage     *storage
	kafkaWriter IKafkaWriter

	recvMsgChan chan *pb.FollowerMsg
	sendMsgChan chan *pb.LeaderMsg
}

func NewStandalone(conf *ClusterCfg) *Standalone {
	kafkaReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: conf.KafkaBrokers,
		GroupID: conf.KafkaGroupId,
		Topic:   conf.KafkaTopic,
		Dialer: &kafka.Dialer{
			Timeout:   kafkaReadTimeout,
			DualStack: true,
		},
	})

	s := &Standalone{
		conf:        conf,
		httpServer:  &http.Server{Handler: conf.Mux},
		sessions:    make(map[string]*pb.Session),
		recvMsgChan: make(chan *pb.FollowerMsg),
		sendMsgChan: make(chan *pb.LeaderMsg),
	}

	if conf.EnableClientMsg {
		s.kafkaWriter = kafka.NewWriter(kafka.WriterConfig{
			Brokers:  conf.KafkaBrokers,
			Topic:    conf.KafkaTopic,
			Balancer: &kafka.Hash{},
			Dialer: &kafka.Dialer{
				Timeout:   kafkaWriteTimeout,
				DualStack: true,
			},
		})
	}

	s.storage = newStorage(conf.EventManager, kafkaReader, s.sendMsgChan, conf.CleanEvents,
		conf.EventTTLDays, conf.EventPayloadMaxBytes)
	return s
}

func (s *Standalone) Run(ctx context.Context, stopNotifyCh chan<- struct{}) {
	glog.Infof("standalone cluster is starting")

	lis, err := net.Listen("tcp", s.conf.Addr)
	if err != nil {
		err := fmt.Errorf("listen %s error: %v", s.conf.Addr, err)
		glog.Error(err)
		panic(err)
	}

	go func() {
		glog.Infof("http server is listening %v", s.conf.Addr)
		if err := s.httpServer.Serve(lis); errors.Is(err, http.ErrServerClosed) {
			glog.Infof("http server closed")
		} else if err != nil {
			err := fmt.Errorf("error serve http mux server: %v", err)
			glog.Error(err)
			panic(err)
		}
	}()

	// clean session ticker.
	ticker := time.NewTicker(5 * time.Minute)

	storageStopDoneC := make(chan struct{})
	hubStopDoneC := make(chan struct{})

	defer func() {
		ticker.Stop()
		s.httpServer.Shutdown(context.Background())
		glog.Infof("standalone cluster: http server shutdown done")

		<-storageStopDoneC
		close(storageStopDoneC)
		glog.Infof("standalone cluster: storage stopped")

		<-hubStopDoneC
		close(hubStopDoneC)
		glog.Infof("standalone cluster: hub stopped")

		close(s.sendMsgChan)
		close(s.recvMsgChan)
		glog.Infof("standalone cluster: stopped")
		stopNotifyCh <- struct{}{}
	}()

	go s.storage.run(ctx, storageStopDoneC)
	go s.conf.Hub.Run(ctx, s.recvMsgChan, s.sendMsgChan, hubStopDoneC)
	s.conf.Hub.Online()

	glog.Infof("standalone cluster is is blocking at recv loop")

	for {
		select {
		case <-ctx.Done():
			s.conf.Hub.Offline()
			glog.Infof("standalone cluster is stopping")
			return
		case <-ticker.C:
			if slice := s.cleanSession(); len(slice) > 0 {
				s.kickoff(slice)
			}
		case msg, ok := <-s.recvMsgChan:
			if !ok {
				return
			}
			if v := msg.SessionOnline; v != nil {
				s.addSession(v)
				if v := s.getUserSessionsToKickoff(v.Uid); len(v) > 0 {
					s.kickoff(v)
				}
			} else if v := msg.SessionOffline; v != "" {
				s.delSession(v)
			} else if v := msg.SyncSessions; len(v) > 0 {
				s.addSessions(v)
			} else if v := msg.E2EMsg; v != nil {
				if err := saveE2eMsg(s.kafkaWriter, v, int(s.conf.EventPayloadMaxBytes)); err != nil {
					// Just ignore.
					glog.Errorf("error save im msg: %v", err)
				}
			} else if msg.ServerShutdown {
				return
			} else {
				panic(fmt.Sprintf("unknown hub message: %#+v", msg))
			}
		}
	}
}

func (s *Standalone) kickoff(sessions []*pb.Session) {
	var sids []string
	now := time.Now().Unix()
	for _, sess := range sessions {
		sess.KickoffTime = now
		sids = append(sids, sess.Sid)
	}
	msg := &pb.LeaderMsg{Kickoff: sids}
	s.sendMsgChan <- msg
}

// order by ctime asc.
func (s *Standalone) getUserSessionsToKickoff(uid int32) []*pb.Session {
	var slice []*pb.Session
	s.Lock()
	for _, sess := range s.sessions {
		if sess.Uid == uid {
			slice = append(slice, sess)
		}
	}
	s.Unlock()

	n := len(slice) - int(s.conf.SessionQuota)
	if n <= 0 {
		return nil
	}

	sort.Slice(slice, func(i, j int) bool {
		return slice[i].CreateTime < slice[j].CreateTime
	})

	return slice[:n]
}

func (s *Standalone) cleanSession() []*pb.Session {
	now := time.Now().Unix()
	s.Lock()
	defer s.Unlock()

	var sidsToDelete []string
	userSessions := make(map[int32][]*pb.Session)

	for _, sess := range s.sessions {
		if sess.KickoffTime > 0 && now > sess.KickoffTime+deleteSinceKickoffTTL {
			sidsToDelete = append(sidsToDelete, sess.Sid)
		}
	}

	for _, sid := range sidsToDelete {
		delete(s.sessions, sid)
	}

	for _, sess := range s.sessions {
		uid := sess.Uid
		userSessions[uid] = append(userSessions[uid], sess)
	}

	var kickoff []*pb.Session

	for _, slice := range userSessions {
		n := len(slice) - int(s.conf.SessionQuota)
		if n <= 0 {
			continue
		}

		sort.Slice(slice, func(i, j int) bool {
			return slice[i].CreateTime < slice[j].CreateTime
		})

		kickoff = append(kickoff, slice[:n]...)
	}
	return kickoff
}

func (s *Standalone) addSession(sess *pb.Session) {
	s.Lock()
	s.sessions[sess.Sid] = sess
	s.Unlock()
}

func (s *Standalone) addSessions(slice []*pb.Session) {
	s.Lock()
	for _, sess := range slice {
		s.sessions[sess.Sid] = sess
	}
	s.Unlock()
}

func (s *Standalone) delSession(sid string) {
	s.Lock()
	delete(s.sessions, sid)
	s.Unlock()
}
