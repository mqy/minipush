package ws

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/glog"
	"github.com/pborman/uuid"

	"github.com/mqy/minipush/auth"
	"github.com/mqy/minipush/cluster"
	pb "github.com/mqy/minipush/proto"
	"github.com/mqy/minipush/store"
)

// Hub works as a hub that manages and serves sessions.
type Hub struct {
	cluster.IHub

	wsConf     *pb.WsConf
	eventApi   *EventApi
	saveImFunc func(ctx context.Context, msg *pb.E2EMsg) error
	authClient auth.Client
	hstore     *HandlerStore
	followerC  chan<- *pb.FollowerMsg
	online     bool
}

// NewHub creates a `Hub`.
func NewHub(authClient auth.Client, eventStore store.IEventStore, wsConf *pb.WsConf) *Hub {
	return &Hub{
		wsConf:     wsConf,
		eventApi:   NewApi(eventStore, wsConf),
		authClient: authClient,
		hstore: &HandlerStore{
			handlers: make(map[string]*Handler),
		},
	}
}

// Start implements `cluster.Follower.Run`.
func (h *Hub) Run(ctx context.Context, followerC chan<- *pb.FollowerMsg, leaderC <-chan *pb.LeaderMsg,
	stopDoneNotifyC chan<- struct{}) {
	h.followerC = followerC

	for {
		select {
		case <-ctx.Done():
			glog.Infof("close connections ...")
			h.hstore.close()
			glog.Infof("close connections done")
			stopDoneNotifyC <- struct{}{}
			return
		case msg, ok := <-leaderC:
			if !ok {
				return
			}

			if glog.V(5) {
				m := jsonpb.Marshaler{}
				out, _ := m.MarshalToString(msg)
				glog.Infof("hub: get server message: %s", out)
			}

			if v := msg.Kickoff; len(v) > 0 {
				for _, sid := range v {
					if s := h.hstore.get(sid); s != nil {
						glog.V(5).Infof("kickoff local session: %s", s)
						s.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{Kickoff: true}})
						h.hstore.del(sid)
					}
				}
			} else if v := msg.HeadSeq; len(v) > 0 {
				for uid, seq := range v {
					if slice := h.hstore.getByUid(uid); len(slice) > 0 {
						for _, s := range slice {
							s.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{HeadSeq: seq}})
						}
					}
				}
			}
		}
	}
}

// ServeHTTP handles websocket requests from the peer.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.online {
		http.Error(w, "This node is temporially disconnected from cluster", http.StatusServiceUnavailable)
		return
	}

	uid, err := h.authClient.Auth(r)
	if err != nil {
		glog.Errorf("ServeHTTP(): authenticate error: %v", err)
		http.Error(w, "Authenticate error", http.StatusForbidden)
		return
	}

	sess := &pb.Session{
		Uid:        uid,
		Sid:        strings.ReplaceAll(uuid.New(), "-", ""),
		CreateTime: time.Now().Unix(),
		Ip:         getRemoteIP(r),
	}

	// If the upgrade fails, then Upgrade replies to the client with an HTTP error response.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		glog.Errorf("ServeHTTP(): upgrader.Upgrade error, uid: %d, err: %s", uid, err)
		return
	}

	// NOTE:  after upgrade, `w.WriteHeader(...)`` causes error `response.Write on hijacked connection`.

	handler := &Handler{
		dataChan: make(chan *SessionData, 16),
		session:  sess,
		conn:     conn,
		eventApi: h.eventApi,
		hub:      h,
	}

	conn.SetCloseHandler(func(code int, text string) error {
		glog.Infof("session closed by peer, session: %s, code: %d, text: %s", handler, code, text)
		h.delHandler(sess.Sid)
		return nil
	})

	h.addHandler(handler)

	go handler.recvLoop()
	go handler.sendLoop()
}

func (h *Hub) addHandler(handler *Handler) {
	h.hstore.add(handler)
	h.followerC <- &pb.FollowerMsg{SessionOnline: handler.session}
}

func (h *Hub) delHandler(sid string) {
	if h.hstore.del(sid) {
		h.followerC <- &pb.FollowerMsg{SessionOffline: sid}
	}
}

func (h *Hub) routeClientMsg(msg *pb.E2EMsg) {
	h.followerC <- &pb.FollowerMsg{E2EMsg: msg}
}

// Online implements `cluster.Follower.Online`
func (h *Hub) Online() {
	glog.Infof("Online()")
	h.online = true

	// Sync local sessions to leader.
	sessions := h.hstore.shallowCopySessions()
	if len(sessions) == 0 {
		return
	}

	glog.V(5).Infof("Online(): sync %d sessions to leader ...", len(sessions))

	const batch = 1000
	size := len(sessions)
	for i := 0; i < size; i += batch {
		j := i + batch
		if j > size {
			j = size
		}
		h.followerC <- &pb.FollowerMsg{
			SyncSessions: sessions[i:j],
		}
	}

	glog.V(5).Infof("Online(): sync %d sessions to leader done.", len(sessions))
}

// Offline implements `cluster.Follower.Offline`
func (h *Hub) Offline() {
	glog.Infof("Offline()")
	h.online = false
}

// Kickoff implements `cluster.Follower.Kickoff`
func (h *Hub) Kickoff(sid string) {
	glog.Infof("Kickoff: %s", sid)
	if s := h.hstore.get(sid); s != nil {
		glog.V(5).Infof("Kickoff(): kickoff local session: %s", s)
		s.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{Kickoff: true}})
		h.hstore.del(sid)
	}
}

func getRemoteIP(r *http.Request) string {
	ip := r.Header.Get("X-REAL-IP")
	if ip == "" {
		if ips := r.Header.Get("X-FORWARDED-FOR"); ips != "" {
			slice := strings.Split(ips, ",")
			for _, x := range slice {
				if x != "" {
					ip = x
				}
			}
		}
	}
	if ip == "" {
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	return ip
}
