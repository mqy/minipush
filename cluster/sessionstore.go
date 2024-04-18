package cluster

import (
	"sort"
	"sync"
	"time"

	pb "github.com/mqy/minipush/proto"
)

const (
	// duration to delete a session since last kickoff
	deleteSinceKickoffTTL = 60 // seconds
)

// memory cluster session store.
type SessionStore struct {
	sync.RWMutex

	// map from hubId -> sid -> session
	kv map[string]map[string]*pb.Session
}

func newSessionStore() *SessionStore {
	return &SessionStore{
		kv: make(map[string]map[string]*pb.Session),
	}
}

func (s *SessionStore) empty() {
	s.Lock()
	s.kv = make(map[string]map[string]*pb.Session)
	s.Unlock()
}

func (s *SessionStore) add(hubId string, sess *pb.Session) {
	s.Lock()
	v, ok := s.kv[hubId]
	if !ok {
		v = make(map[string]*pb.Session)
		s.kv[hubId] = v
	}
	sess.HubId = hubId
	v[sess.Sid] = sess
	s.Unlock()
}

func (s *SessionStore) addMany(hubId string, slice []*pb.Session) {
	s.Lock()
	v, ok := s.kv[hubId]
	if !ok {
		v = make(map[string]*pb.Session)
		s.kv[hubId] = v
	}
	for _, sess := range slice {
		sess.HubId = hubId
		v[sess.Sid] = sess
	}
	s.Unlock()
}

func (s *SessionStore) del(hubId, sid string) {
	s.Lock()
	if v, ok := s.kv[hubId]; ok {
		delete(v, sid)
	}
	s.Unlock()
}

// order by ctime asc.
func (s *SessionStore) getUserSessionsToKickoff(uid int32, quota int32) map[string]*pb.Session {
	var slice []*pb.Session
	s.Lock()
	for _, v := range s.kv {
		for _, sess := range v {
			if sess.Uid == uid {
				slice = append(slice, sess)
			}
		}
	}
	s.Unlock()

	n := len(slice) - int(quota)
	if n <= 0 {
		return nil
	}

	sort.Slice(slice, func(i, j int) bool {
		return slice[i].CreateTime < slice[j].CreateTime
	})

	kickoff := make(map[string]*pb.Session)
	for _, sess := range slice[:n] {
		kickoff[sess.Sid] = sess
	}
	return kickoff
}

// `gc` deletes: sessions in unreachable (down) hubs, or sessions hub not deleted after kickoff.
// `gc` does not take care of TTL of live sessions: that's the responsibility of the own hub.
// Returns live user sessions to kickoff.
func (s *SessionStore) gc(liveHubIds map[string]struct{}, quota int32) map[string]*pb.Session {
	now := time.Now().Unix()
	s.Lock()
	defer s.Unlock()

	userSessions := make(map[int32][]*pb.Session)

	for k, v := range s.kv {
		if _, ok := liveHubIds[k]; !ok {
			delete(s.kv, k)
		} else {
			for sid, sess := range v {
				if sess.KickoffTime > 0 && now > sess.KickoffTime+deleteSinceKickoffTTL {
					delete(v, sid)
				}
			}
		}
	}

	for _, v := range s.kv {
		for _, sess := range v {
			uid := sess.Uid
			userSessions[uid] = append(userSessions[uid], sess)
		}
	}

	kickoff := make(map[string]*pb.Session)

	for _, slice := range userSessions {
		n := len(slice) - int(quota)
		if n <= 0 {
			continue
		}

		sort.Slice(slice, func(i, j int) bool {
			return slice[i].CreateTime < slice[j].CreateTime
		})

		for _, sess := range slice[:n] {
			kickoff[sess.Sid] = sess
		}
	}
	return kickoff
}
