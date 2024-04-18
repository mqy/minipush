package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang/glog"

	pb "github.com/mqy/minipush/proto"
)

const (
	getEventSQL  = "SELECT create_time,payload FROM events WHERE topic_offset=?"
	getEventsSQL = "SELECT u.seq, u.create_time, u.read_state, e.payload " +
		"FROM user_events AS u, events AS e " +
		"WHERE u.uid = ? AND u.seq >= ? AND u.seq <= ? AND u.topic_offset = e.topic_offset " +
		"ORDER BY u.seq DESC"
	getSeqSQL        = "SELECT seq FROM user_seq WHERE uid=?"
	lockSeqSQL       = "SELECT seq FROM user_seq WHERE uid=? FOR UPDATE"
	insertSeqSQL     = "INSERT INTO user_seq (uid, seq) VALUES (?, 1)"
	incSeqSQL        = "UPDATE user_seq SET seq=seq+1 WHERE uid=? AND seq=?"
	countUnreadSQL   = "SELECT COUNT(uid) FROM user_events WHERE uid = ? AND create_time >= ? AND read_state = 0"
	getReadStatesSQL = "SELECT seq,read_state FROM user_events WHERE uid = ? AND seq <= ? AND create_time >= ? ORDER BY seq DESC"
	setReadSQL       = "UPDATE user_events SET read_state = 1 WHERE uid = ? AND seq = ? AND read_state = 0"
)

const (
	insertEventSQL     = "INSERT INTO events (topic_offset,create_time,payload) VALUES (?,?,?)"
	cleanEventsSQL     = "DELETE FROM events WHERE create_time <= ?"
	insertUserEventSQL = "INSERT INTO user_events (uid, seq, create_time, topic_offset) VALUES (?,?,?,?)"
	getMaxSeqsSQL      = "SELECT uid, MAX(seq) FROM user_events WHERE uid IN (%s) GROUP BY uid"
)

// eventStore implements interface `IEventStore` and `IEventManager`.
type eventStore struct {
	*sql.DB
}

func NewEventStore(db *sql.DB) *eventStore {
	return &eventStore{db}
}

func (s *eventStore) withTx(ctx context.Context, exec func(ctx context.Context, tx *sql.Tx) error, opts ...*sql.TxOptions) error {
	var txOpts *sql.TxOptions
	if len(opts) == 0 {
		txOpts = &sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
			ReadOnly:  false,
		}
	} else {
		txOpts = opts[0]
	}
	tx, err := s.BeginTx(ctx, txOpts)
	if err != nil {
		return err
	}

	if err := exec(ctx, tx); err != nil {
		err2 := tx.Rollback()
		if err2 != nil {
			glog.Errorf("failed to rollback: %v", err)
		}
		return err
	}

	return tx.Commit()
}

func (s *eventStore) getSeq(ctx context.Context, tx *sql.Tx, uid int32) (int32, error) {
	row := tx.QueryRowContext(ctx, getSeqSQL, uid)
	var out sql.NullInt32
	if err := row.Scan(&out); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		glog.Errorf("get seq scan err: %v", err)
		return 0, err
	}
	if out.Valid {
		return out.Int32, nil
	}
	return 0, nil
}

func (s *eventStore) countUnread(ctx context.Context, tx *sql.Tx, uid, days int32) (int32, error) {
	var lteCreateTime = GetDayBefore(days).Unix()
	row := tx.QueryRowContext(ctx, countUnreadSQL, uid, lteCreateTime)
	var out sql.NullInt32
	if err := row.Scan(&out); err != nil {
		glog.Errorf("count unread scan err: %v", err)
		return 0, err
	}
	if out.Valid {
		return out.Int32, nil
	}
	return 0, nil
}

func (s *eventStore) Stats(ctx context.Context, uid, days int32, req *pb.StatsReq) (*pb.StatsResp, error) {
	var out pb.StatsResp

	if err := s.withTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		seq, err := s.getSeq(ctx, tx, uid)
		if err != nil {
			return err
		}
		out.HeadSeq = seq

		if req.CountUnread {
			count, err := s.countUnread(ctx, tx, uid, days)
			if err != nil {
				return err
			}
			out.UnreadCount = count
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &out, nil
}

func (s *eventStore) GetEvents(ctx context.Context, uid int32, req *pb.GetEventsReq) (*pb.GetEventsResp, error) {
	var events []*pb.Event
	if err := s.withTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, getEventsSQL, uid, req.FromSeq, req.ToSeq)
		if err != nil {
			glog.Errorf("get events query err: %v", err)
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var e pb.Event
			var t time.Time
			if err := rows.Scan(&e.Seq, &t, &e.ReadState, &e.Payload); err != nil {
				glog.Errorf("get events scan err: %v", err)
				return err
			}
			e.Uid = uid
			e.CreateTime = t.Unix()
			events = append(events, &e)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return &pb.GetEventsResp{Events: events}, nil
}

func (s *eventStore) GetReadStates(ctx context.Context, uid, days int32, req *pb.GetReadStatesReq) (*pb.GetReadStatesResp, error) {
	var seqSlice []int32
	var readStates []bool

	var lteCreateTime = GetDayBefore(days).Unix()

	if err := s.withTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, getReadStatesSQL, uid, req.HeadSeq, lteCreateTime)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var seq int32
			var readState byte

			if err := rows.Scan(&seq, &readState); err != nil {
				return err
			}

			seqSlice = append(seqSlice, seq)
			readStates = append(readStates, readState > 0)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return MakeGetReadStatesResp(seqSlice, readStates)
}

func (s *eventStore) SetRead(ctx context.Context, uid, seq int32) (*pb.SetReadResp, error) {
	var changed bool
	if err := s.withTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, setReadSQL, uid, seq)
		if err != nil {
			return err
		}

		n, _ := res.RowsAffected()
		changed = n == 1
		return nil
	}); err != nil {
		return nil, err
	}

	return &pb.SetReadResp{
		Seq:     seq,
		Changed: changed,
	}, nil
}

func (s *eventStore) IsDupKeyError(err error) bool {
	if val, ok := err.(*mysql.MySQLError); ok {
		return val.Number == 1062
	}
	return false
}

func (s *eventStore) Save(ctx context.Context, e *Event) (map[int32]int32, error) {
	seqMap := make(map[int32]int32)
	if err := s.withTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, insertEventSQL, e.Offset, e.CreateTime, e.Payload); err != nil {
			// Duplicate key error MUST be caused by failure of commit kafka message.
			if s.IsDupKeyError(err) {
				// If this event has been saved, then skip saving.
				var payload string
				var createTime time.Time
				row := tx.QueryRowContext(ctx, getEventSQL, e.Offset)
				if err := row.Scan(&createTime, &payload); err != nil {
					glog.Errorf("get event error, offset: %d, err: %v", e.Offset, err)
				} else if createTime.UnixNano()/1e6 == e.CreateTime.UnixNano()/1e6 && payload == e.Payload { // equals
					return nil
				}
			}
			return err
		}

		stmt, err := tx.PrepareContext(ctx, insertUserEventSQL)
		if err != nil {
			glog.Errorf("prepare insert user event err: %v", err)
			return err
		}
		defer stmt.Close()

		for _, uid := range e.To {
			seq, err := s.incSeq(ctx, tx, uid)
			if err != nil {
				return err
			}

			if _, err = stmt.ExecContext(ctx, uid, seq, e.CreateTime, e.Offset); err != nil {
				glog.Errorf("insert user event exec err: %v", err)
				return err
			}
			seqMap[uid] = seq
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelSerializable}); err != nil {
		return nil, err
	}
	return seqMap, nil
}

func (s *eventStore) incSeq(ctx context.Context, tx *sql.Tx, uid int32) (int32, error) {
	var seq int32

	// select for update
	row := tx.QueryRowContext(ctx, lockSeqSQL, uid)
	if err := row.Scan(&seq); err != nil {
		if err != sql.ErrNoRows {
			glog.Errorf("get seq scan err: %v", err)
			return -1, err
		}
	}

	// insert if not found.
	if seq == 0 {
		if _, err := tx.ExecContext(ctx, insertSeqSQL, uid); err != nil {
			if s.IsDupKeyError(err) {
				// already exits, select for update again.
				row := tx.QueryRowContext(ctx, lockSeqSQL, uid)
				if err := row.Scan(&seq); err != nil {
					return -1, err
				}
			} else {
				glog.Errorf("insert seq err: %v", err)
				return -1, err
			}
		}
	}

	if _, err := tx.ExecContext(ctx, incSeqSQL, uid, seq); err != nil {
		glog.Errorf("update seq exec err: %v", err)
		return -1, err
	}
	return seq + 1, nil
}

func (s *eventStore) DeleteOutdatedEvents(ctx context.Context, ttlDays int32) (int32, error) {
	t := GetDayBefore(ttlDays)
	// Max value of create_time to match.
	var lteCreateTime = t.Unix()
	var numDeleted int32

	if err := s.withTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, cleanEventsSQL, lteCreateTime)
		if err != nil {
			return err
		}

		n, err := res.RowsAffected()
		if err != nil {
			return err
		}

		numDeleted = int32(n)
		return nil
	}); err != nil {
		return 0, err
	}
	return numDeleted, nil
}
