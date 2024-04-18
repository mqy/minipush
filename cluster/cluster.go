package cluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	kafka "github.com/segmentio/kafka-go"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"

	pb "github.com/mqy/minipush/proto"
	"github.com/mqy/minipush/store"
)

const (
	kafkaReadTimeout  = 10 * time.Second
	kafkaWriteTimeout = 10 * time.Second
)

type ClusterCfg struct {
	Pid  int
	Addr string
	Hub  IHub
	Mux  *http.ServeMux

	RunLeader   bool
	RunFollower bool
	LeaderAddr  string

	KafkaBrokers []string
	KafkaTopic   string
	KafkaGroupId string

	EnableClientMsg bool

	EventManager         store.IEventStore
	CleanEvents          bool
	EventTTLDays         int32
	EventPayloadMaxBytes int32

	SessionQuota int32
}

type Cluster struct {
	ICluster
	conf  *ClusterCfg
	hubId string

	grpcServer     *grpc.Server
	pushGrpcServer *pushServer
	httpServer     *http.Server

	recvMsgChan chan *pb.FollowerMsg
	sendMsgChan chan *pb.LeaderMsg

	kafkaWriter IKafkaWriter

	wg sync.WaitGroup
}

func NewCluster(conf *ClusterCfg) *Cluster {
	m := &Cluster{
		hubId:       fmt.Sprintf("%s_%d", conf.Addr, conf.Pid),
		conf:        conf,
		recvMsgChan: make(chan *pb.FollowerMsg, 8),
		sendMsgChan: make(chan *pb.LeaderMsg, 8),
	}

	if conf.EnableClientMsg {
		m.kafkaWriter = kafka.NewWriter(kafka.WriterConfig{
			Brokers:  m.conf.KafkaBrokers,
			Topic:    m.conf.KafkaTopic,
			Balancer: &kafka.Hash{},
			Dialer: &kafka.Dialer{
				Timeout:   kafkaWriteTimeout,
				DualStack: true,
			},
		})
	}

	var handler http.Handler
	if m.conf.RunLeader {
		m.pushGrpcServer = newPushServer(m.sendMsgChan, m.kafkaWriter, conf.SessionQuota, conf.EventPayloadMaxBytes)
		m.grpcServer = grpc.NewServer()
		pb.RegisterMiniPushClusterServer(m.grpcServer, m.pushGrpcServer)

		handler = h2c.NewHandler(m, &http2.Server{})
	} else {
		handler = m
	}

	m.httpServer = &http.Server{Handler: handler}
	return m
}

func (m *Cluster) Run(ctx context.Context, stopNotifyCh chan<- struct{}) {
	lis, err := net.Listen("tcp", m.conf.Addr)
	if err != nil {
		err := fmt.Errorf("listen %s error: %v", m.conf.Addr, err)
		glog.Error(err)
		panic(err)
	}

	go func() {
		if err := m.httpServer.Serve(lis); errors.Is(err, http.ErrServerClosed) {
			glog.Infof("http server closed")
		} else if err != nil {
			err := fmt.Errorf("http Serve error: %v", err)
			glog.Error(err)
			panic(err)
		}
	}()

	glog.Infof("cluster: %v run leader", m.conf.Addr)

	if m.conf.RunLeader {
		go m.runLeader(ctx)
	}

	glog.Infof("cluster: %v run follower", m.conf.Addr)
	if m.conf.RunFollower {
		go m.runClient(ctx)
	}

	m.wg.Wait()
	stopNotifyCh <- struct{}{}
}

func (m *Cluster) runLeader(ctx context.Context) {
	m.wg.Add(1)
	storageStopDoneC := make(chan struct{})
	serverStopDoneC := make(chan struct{})
	go m.pushGrpcServer.run(ctx, serverStopDoneC)

	defer func() {
		glog.Infof("cluster: stopping")

		if m.conf.RunLeader {
			glog.Infof("leader: stopping")
			m.sendMsgChan <- &pb.LeaderMsg{ServerShutdown: true}
			<-storageStopDoneC
			close(storageStopDoneC)
			<-serverStopDoneC

			func() {
				defer func() {
					// Known error: Drain() is not implemented
					// See: https://github.com/grpc/grpc-go/issues/1384
					if err := recover(); err != nil {
						glog.Errorf("grpc server GracefulStop panic: %v, recovered", err)
					}
				}()
				m.grpcServer.GracefulStop()
				glog.Infof("grpc server graceful stopped")
			}()
			glog.Infof("leader stopped")
		}

		m.httpServer.Shutdown(context.Background())
		glog.Infof("cluster: http server stopped")

		close(m.sendMsgChan)
		close(m.recvMsgChan)

		glog.Infof("cluster: stopped")
		m.wg.Done()
	}()

	kafkaReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: m.conf.KafkaBrokers,
		GroupID: m.conf.KafkaGroupId,
		Topic:   m.conf.KafkaTopic,
		Dialer: &kafka.Dialer{
			Timeout:   kafkaReadTimeout,
			DualStack: true,
		},
	})

	s := newStorage(m.conf.EventManager, kafkaReader, m.sendMsgChan,
		m.conf.CleanEvents, m.conf.EventTTLDays, m.conf.EventPayloadMaxBytes)
	s.run(ctx, storageStopDoneC)
}

func (m *Cluster) runClient(ctx context.Context) {
	m.wg.Add(1)
	var client *client
	var leader string

	if m.conf.RunLeader {
		leader = m.conf.Addr
	} else {
		leader = m.conf.LeaderAddr
	}

	glog.Infof("cluster: client connecting %s to leader %s", m.conf.Addr, leader)

	hubStopDoneC := make(chan struct{})
	if m.conf.RunFollower {
		go m.conf.Hub.Run(ctx, m.recvMsgChan, m.sendMsgChan, hubStopDoneC)
	}

	defer func() {
		glog.Infof("client: stopping")
		client.stop() // it may took 7 seconds to close kafka reader.
		<-hubStopDoneC
		close(hubStopDoneC)
		glog.Infof("client: stopped")
		m.wg.Done()
	}()

	var err error
	client, err = newClient(ctx, leader, m.hubId, m.recvMsgChan, m.sendMsgChan)
	if err != nil {
		panic(fmt.Errorf("failed to create client: %v", err))
	}

	m.conf.Hub.Online()
	client.start()
	m.conf.Hub.Offline()
}

func (m *Cluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("content-type"), "application/grpc") {
		if m.conf.RunLeader {
			m.grpcServer.ServeHTTP(w, r)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	} else {
		m.conf.Mux.ServeHTTP(w, r)
	}
}
