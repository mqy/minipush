package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/glog"
	"github.com/gorilla/websocket"

	pb "github.com/mqy/minipush/proto"
)

type SessionError int

const (
	ReadError  SessionError = 1
	WriteError SessionError = 2
	PingError  SessionError = 3
	BadRequest SessionError = 4
	ServerStop SessionError = 5
	KickedOff  SessionError = 6
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 3 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	// Recommend configure nginx with `keep-alive_timeout` >= 65s.
	pingPeriod = 20 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 25 * time.Second

	// websocket max message size to read.
	readLimit = 4096
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	// Fix error: request origin not allowed by Upgrader.CheckOrigin
	CheckOrigin: func(r *http.Request) bool {
		// When the node is behind nginx: host=ws-backend, see dev/nginx.conf.
		// TODO: possible SECURITY LEAK.
		return true
	},
}

// Handler managers an active connection to end user.
// Every new websocket connection creates a new session.
type Handler struct {
	sync.Mutex

	eventApi *EventApi
	hub      *Hub

	session *pb.Session
	conn    *websocket.Conn

	dataChan chan *SessionData

	// TODO: stopChan
	closing bool
}

// SessionData is the data structure for `dataChan`.
type SessionData struct {
	Error     SessionError  `json:"error,omitempty"`
	ServerMsg *pb.ServerMsg `json:"resp,omitempty"`
}

func (h *Handler) String() string {
	m := jsonpb.Marshaler{OrigName: true}
	out, _ := m.MarshalToString(h.session)
	return out
}

func (h *Handler) close(cause SessionError) {
	h.Lock()
	defer h.Unlock()
	if h.closing {
		return
	}

	h.closing = true

	h.conn.SetWriteDeadline(time.Now().Add(writeWait))
	_ = h.conn.WriteMessage(websocket.CloseMessage, []byte{})
	h.conn.Close()

	close(h.dataChan)

	if cause != ServerStop {
		glog.V(5).Infof("session closed, cause: %d, %s", cause, h)
		// Ask for node to remove this handler.
		h.hub.delHandler(h.session.Sid)
	}
}

func (h *Handler) appendDataChan(v *SessionData) {
	h.Lock()
	defer h.Unlock()
	if !h.closing {
		h.dataChan <- v
	}
}

func sendServerMsg(conn *websocket.Conn, msg *pb.ServerMsg) error {
	m := jsonpb.Marshaler{OrigName: true}
	out, _ := m.MarshalToString(msg)
	conn.SetWriteDeadline(time.Now().Add(writeWait))
	return conn.WriteMessage(websocket.TextMessage, []byte(out))
}

func (h *Handler) recvLoop() {
	defer func() { glog.V(5).Infof("recvLoop(): exited, session: %s", h.String()) }()

	h.conn.SetReadLimit(readLimit)
	h.conn.SetReadDeadline(time.Now().Add(pongWait))
	h.conn.SetPongHandler(func(s string) error {
		//glog.V(5).Infof("session get pong: %s", h.String())
		h.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for !h.closing {
		msgType, msg, err := h.conn.ReadMessage()
		if err != nil {
			glog.Errorf("recvLoop(): read error: %v", err)
			h.appendDataChan(&SessionData{Error: ReadError})
			return
		}

		glog.V(5).Infof("recvLoop(): incoming client message: %v", string(msg))

		if msgType != websocket.TextMessage {
			glog.Errorf("recvLoop(): unexpected message type: %d", msgType)
			h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{
				Error: newInvalidArgumentError(nil, "websocket only supports TextMessage"),
			}})
			h.appendDataChan(&SessionData{Error: BadRequest})
			return
		}

		req := pb.ClientMsg{}
		if err := json.Unmarshal(msg, &req); err != nil {
			glog.Errorf("recvLoop(): message error: msg: %s, err: %v", string(msg), err)
			h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{
				Error: newInvalidArgumentError(&req, fmt.Sprintf("marshal error: %v", err)),
			}})
			h.appendDataChan(&SessionData{Error: BadRequest})
			return
		}

		uid := h.session.Uid

		if v := req.Stats; v != nil {
			resp, err := h.eventApi.Stats(context.Background(), uid, v)
			if err != nil {
				glog.Errorf("recvLoop(): Stats error: %+v", err)
				interceptError(err)
				h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{Error: err}})
				continue
			}
			h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{Stats: resp}})
		} else if v := req.GetEvents; v != nil {
			resp, err := h.eventApi.GetEvents(context.Background(), uid, v)
			if err != nil {
				glog.Errorf("recvLoop(): GetEvents error: %+v", err)
				interceptError(err)
				h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{Error: err}})
				continue
			}
			h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{GetEvents: resp}})
		} else if v := req.SetRead; v != nil {
			resp, err := h.eventApi.SetRead(context.Background(), uid, v)
			if err != nil {
				glog.Errorf("recvLoop(): SetRead error: %+v", err)
				interceptError(err)
				h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{Error: err}})
				continue
			}
			h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{SetRead: resp}})
		} else if v := req.GetReadStates; v != nil {
			resp, err := h.eventApi.GetReadStates(context.Background(), uid, v)
			if err != nil {
				glog.Errorf("recvLoop(): GetReadStates error: %+v", err)
				interceptError(err)
				h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{Error: err}})
				continue
			}
			h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{GetReadStates: resp}})
		} else if v := req.Im; v != nil {
			if h.hub.wsConf.EnableClientMsg {
				h.hub.routeClientMsg(v)
			} else {
				err := &pb.Error{
					Code:   ErrorCodeInvalidArguments,
					Params: []string{"feature is not supported"},
				}
				h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{Error: err}})
			}
		} else {
			glog.Errorf("recvLoop(): unsupported request: %+v", v)
			h.appendDataChan(&SessionData{ServerMsg: &pb.ServerMsg{
				Error: newInvalidArgumentError(&req, "unsupported request"),
			}})
			h.appendDataChan(&SessionData{Error: BadRequest})
		}
	}
}

func (h *Handler) sendLoop() {
	pingTicker := time.NewTicker(pingPeriod)
	defer func() {
		pingTicker.Stop()
		glog.V(5).Infof("sendLoop(): exited, session: %s", h.String())
	}()

	for {
		select {
		case v, ok := <-h.dataChan:
			if !ok { // chan was closed
				h.conn.Close()
				glog.V(5).Infof("sendLoop(): data chan closed, session: %s", h.String())
				return
			}

			if glog.V(5) {
				dataJson, _ := json.Marshal(v)
				logValue := string(dataJson)
				if len(logValue) > 100 {
					logValue = logValue[:100] + " ..."
				}
				glog.Infof("sendLoop(), get from data chan, value: %s, session: %s", logValue, h.String())
			}

			if v.Error > 0 {
				h.close(v.Error)
				return
			} else if v.ServerMsg == nil {
				// should not happen.
				panic(fmt.Sprintf("sendLoop(), unknown data from dataChan: %#+v", v))
			}

			if err := sendServerMsg(h.conn, v.ServerMsg); err != nil {
				glog.Errorf("sendLoop(), error write message. session: %s, resp: %s, err: %v",
					h.String(), v.ServerMsg, err)
				h.appendDataChan(&SessionData{Error: WriteError})
				return
			}
			if v.ServerMsg.Kickoff {
				h.close(KickedOff)
			}
		case <-pingTicker.C:
			//glog.V(5).Infof("session send ping: %s", h.String())
			h.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := h.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				glog.Errorf("sendLoop(), error write ping message. session: %s, err: %v", h, err)
				h.appendDataChan(&SessionData{Error: PingError})
				return
			}
		}
	}
}
