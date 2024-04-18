package ws

import (
	"sync"

	pb "github.com/mqy/minipush/proto"
)

// memory handler store for local sessions.
type HandlerStore struct {
	sync.RWMutex
	handlers map[string]*Handler
}

func (hs *HandlerStore) get(sid string) *Handler {
	hs.RLock()
	h := hs.handlers[sid]
	hs.RUnlock()
	return h
}

func (hs *HandlerStore) del(sid string) bool {
	hs.Lock()
	defer hs.Unlock()
	if _, ok := hs.handlers[sid]; ok {
		delete(hs.handlers, sid)
		return true
	}
	return false
}

func (hs *HandlerStore) add(handler *Handler) {
	hs.Lock()
	sid := handler.session.Sid
	hs.handlers[sid] = handler
	hs.Unlock()
}

func (hs *HandlerStore) getByUid(uid int32) []*Handler {
	hs.RLock()
	defer hs.RUnlock()

	var out []*Handler
	for _, h := range hs.handlers {
		if h.session.Uid == uid {
			out = append(out, h)
		}
	}
	return out
}

func (hs *HandlerStore) shallowCopySessions() []*pb.Session {
	hs.RLock()
	defer hs.RUnlock()
	out := make([]*pb.Session, 0, len(hs.handlers))
	for _, s := range hs.handlers {
		out = append(out, s.session)
	}
	return out
}

func (hs *HandlerStore) close() {
	hs.RLock()
	defer hs.RUnlock()
	for _, h := range hs.handlers {
		h.close(ServerStop)
	}
}
