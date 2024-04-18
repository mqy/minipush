package cluster

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/golang/glog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/mqy/minipush/proto"
)

// client is the cluster client, being created  by `ws.Hub` on new leader.
// Every minipush server instance should have just one client instance.
// A client connects to gRPC server on behave of `node.Hub`.
// A client can be created even if current node is leader.
// Life time of a client instance must not be greater than ws.Hub.
type client struct {
	ctx         context.Context
	ctx_cancel  context.CancelFunc
	leaderAddr  string
	recvMsgChan <-chan *pb.FollowerMsg
	sendMsgChan chan<- *pb.LeaderMsg

	clientConn  *grpc.ClientConn
	routeClient pb.MiniPushCluster_RouteClient
	wg          sync.WaitGroup
}

func newClient(ctx context.Context, leaderAddr, hubId string, recvMsgChan <-chan *pb.FollowerMsg,
	sendMsgChan chan<- *pb.LeaderMsg) (*client, error) {

	ctx2, cancel2 := context.WithCancel(ctx)
	m := &client{
		ctx:         ctx2,
		ctx_cancel:  cancel2,
		leaderAddr:  leaderAddr,
		recvMsgChan: recvMsgChan,
		sendMsgChan: sendMsgChan,
	}

	{ // dial
		dialOpts := []grpc.DialOption{grpc.WithInsecure(), grpc.WithBlock(), grpc.WithDefaultCallOptions(grpc.WaitForReady(true))}
		ctx3, cancel3 := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel3()

		conn, err := grpc.DialContext(ctx3, m.leaderAddr, dialOpts...)
		if err != nil {
			glog.Errorf("client: dial `%s` error: %v", m.leaderAddr, err)
			panic("abort due to dial leader error")
		}

		m.clientConn = conn
	}

	{ // call gRPC API Route()
		kv := map[string]string{"x-hub-id": hubId}
		md := metadata.New(kv)
		ctx4 := metadata.NewOutgoingContext(ctx2, md)
		client := pb.NewMiniPushClusterClient(m.clientConn)
		cli, err := client.Route(ctx4, grpc.WaitForReady(true))
		if err != nil {
			return nil, fmt.Errorf("error call client.Route: %v", err)
		}
		m.routeClient = cli
	}

	return m, nil
}

func (m *client) start() {
	m.wg.Add(1)
	go m.sendLoop()

	glog.Info("client: start() blocking recv")
	defer func() {
		glog.Info("client: start() exited")
		m.wg.Done()
	}()

	for {
		msg, err := m.routeClient.Recv()
		if err != nil {
			glog.Errorf("client: stream recv error: %v", err)
			code := status.Code(err)
			if err == io.EOF || code == codes.Canceled || code == codes.Unavailable { // TODO: verify
				glog.Error("client: abort on EOF or cancel")
				return
			}
			time.Sleep(time.Second)
		} else {
			glog.V(5).Infof("client: received message: %v", msg)
			if msg.ServerShutdown {
				glog.Error("client: abort run because leader server is shutting down")
				go m.stop()
				return
			} else {
				m.sendMsgChan <- msg
			}
		}
	}
}

func (m *client) stop() {
	m.ctx_cancel()
	m.clientConn.Close()

	m.wg.Wait()
}

func (m *client) sendLoop() {
	glog.Info("client: sendLoop(): enter")
	m.wg.Add(1)
	defer func() {
		m.routeClient.CloseSend()
		m.clientConn.Close() // trigger recv loop exit.
		glog.Info("sendLoop(): exited")
		m.wg.Done()
	}()

	for {
		select {
		case <-m.ctx.Done():
			return
		case msg, ok := <-m.recvMsgChan:
			if !ok {
				return
			}
			glog.V(5).Infof("client: received msg: %+v", msg)
			if err := m.routeClient.SendMsg(msg); err != nil {
				glog.Errorf("client: error send msg: %v", err)
			}
		}
	}
}
