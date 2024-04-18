package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/golang/glog"
	kafka "github.com/segmentio/kafka-go"

	pb "github.com/mqy/minipush/proto"
	"github.com/mqy/minipush/store"
)

const (
	// TODO: configure kafka with `max.message.bytes`
	storeSendInterval   = time.Second
	storeCacheTTL       = 5 * time.Second
	storeDeleteInterval = time.Hour

	BackoffMinInterval = 1 * time.Second
	BackoffMaxInterval = 60 * time.Second
	BackoffMultiplier  = 1.5
)

// kafka message value.
type KafkaMsgValue struct {
	Body   string  `json:"body,omitempty"` // event body in JSON format
	ToUids []int32 `json:"uids,omitempty"`
}

type userSeq struct {
	headSeq   int32
	counter   int
	cacheTime time.Time
}

// cache acts as flow controller.
// When there are many unconsumed events in kafka on start, without flow control client will
// be flooded.
type cache struct {
	sync.Mutex
	// uid -> UserSeq
	kv       map[int32]*userSeq
	pushChan chan<- *pb.LeaderMsg
	sending  bool
}

// storage consumes incoming business events from kafka, then splits and inserts to store.
// It also periodically deletes outdated events when the `delEventsTtlDays` > 0.
// There MUST have exactly one storage instance in cluster.
type storage struct {
	sync.Mutex
	es               store.IEventStore
	cleanEvents      bool
	delEventsTtlDays int32
	valueMaxBytes    int32
	kafkaReader      IKafkaReader
	wg               sync.WaitGroup
	cache            *cache
}

func newStorage(es store.IEventStore, kafkaReader IKafkaReader, pushChan chan<- *pb.LeaderMsg,
	cleanEvents bool, eventsTtlDays, valueMaxBytes int32) *storage {

	var delEventsTtlDays int32 = math.MaxInt32
	if cleanEvents {
		delEventsTtlDays = eventsTtlDays
	}

	return &storage{
		es:               es,
		cleanEvents:      cleanEvents,
		delEventsTtlDays: delEventsTtlDays,
		valueMaxBytes:    valueMaxBytes,
		kafkaReader:      kafkaReader,
		cache:            newCache(pushChan),
	}
}

// run consumes events from kafka: save event and send to listener.
// It may block at reading kafka message.
func (s *storage) run(ctx context.Context, stopDoneNotifyC chan<- struct{}) {
	glog.Info("run(): enter")

	go s.consumeLoop(ctx)
	go s.sendLoop(ctx)
	if s.cleanEvents {
		go s.deleteLoop(ctx)
	}

	glog.Info("storage: ready")

	<-ctx.Done()

	glog.Info("storage: stopping")
	_ = s.kafkaReader.Close() // slow: take about 7s

	glog.Info("storage: stop wait")
	s.wg.Wait()

	glog.Info("storage: stopped")
	stopDoneNotifyC <- struct{}{}
}

// deleteLoop deletes outdated events.
func (s *storage) deleteLoop(ctx context.Context) {
	s.wg.Add(1)
	glog.Info("storage: delete loop enter")

	ticker := time.NewTicker(storeDeleteInterval)
	defer func() {
		ticker.Stop()
		glog.Info("storage: delete loop exit")
		s.wg.Done()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			start := time.Now()
			n, err := s.es.DeleteOutdatedEvents(context.Background(), s.delEventsTtlDays)
			if err == nil {
				glog.Infof("storage: deleted %d outdated events, took %s", n, time.Since(start))
			} else {
				glog.Errorf("storage: delete outdated event error: %v ", err)
			}
		}
	}
}

func newCache(pushChan chan<- *pb.LeaderMsg) *cache {
	return &cache{
		kv:       make(map[int32]*userSeq),
		pushChan: pushChan,
	}
}

func (c *cache) put(data map[int32]int32) {
	glog.V(7).Infof("storage: new head seq: %+v", data)
	c.Lock()
	for uid, seq := range data {
		if v, ok := c.kv[uid]; ok {
			if v.headSeq < seq {
				v.headSeq = seq
				v.counter++
			} else {
				panic(fmt.Sprintf("uid: %d, incoming seq `%d` is not bigger than last `%d`", uid, seq, v.headSeq))
			}
		} else {
			c.kv[uid] = &userSeq{
				counter:   1,
				headSeq:   seq,
				cacheTime: time.Now(),
			}
		}
	}
	c.Unlock()
}

func (c *cache) check() {
	c.Lock()
	if c.sending {
		c.Unlock()
		return
	} else {
		c.sending = true
		c.Unlock()
	}

	out := make(map[int32]int32)
	for uid, v := range c.kv {
		dur := time.Since(v.cacheTime)
		freq := float64(v.counter) / dur.Seconds()
		if freq <= 1 || dur > storeCacheTTL {
			glog.V(7).Infof("storage: will send: uid: %d, freq: %f, dur: %s", uid, freq, dur)
			out[uid] = v.headSeq
			delete(c.kv, uid)
		} else {
			glog.V(7).Infof("storage: ignore send: uid: %d, freq: %f, dur: %s", uid, freq, dur)
		}
	}
	if len(out) > 0 {
		glog.V(7).Infof("storage: send: %+v", out)
		c.pushChan <- &pb.LeaderMsg{HeadSeq: out}
		glog.V(7).Infof("storage: send done")
	}

	c.Lock()
	c.sending = false
	c.Unlock()
}

// sendLoop try sends cached head seq periodically.
func (s *storage) sendLoop(ctx context.Context) {
	s.wg.Add(1)
	glog.Info("storage: send loop enter")

	ticker := time.NewTicker(storeSendInterval)
	defer func() {
		ticker.Stop()
		glog.Info("storage: send loop exit")
		s.wg.Done()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cache.check()
		}
	}
}

func (s *storage) consumeLoop(ctx context.Context) {
	glog.Info("storage: consume loop enter")
	s.wg.Add(1)

	defer func() {
		glog.Info("storage: consume loop exited")
		s.wg.Done()
	}()

	var sleep time.Duration

	for {
		glog.V(5).Info("storage: fetching message ...")
		msg, err := s.kafkaReader.FetchMessage(ctx)
		glog.V(5).Info("storage: fetch message done")

		if err == nil {
			sleep = 0
			// skip: bad format or too old.
			if value := s.decodeKafkaMsg(&msg); value != nil {
				e := &store.Event{
					Offset:     msg.Offset,
					CreateTime: msg.Time,
					Payload:    string(msg.Value),
					To:         value.To,
				}

				for {
					glog.V(5).Infof("storage: saving %v", e)
					res, err := s.es.Save(ctx, e)
					if err == nil {
						sleep = 0
						if len(res) > 0 {
							s.cache.put(res)
						}
						for {
							if err := s.kafkaReader.CommitMessages(ctx, msg); err == nil {
								sleep = 0
								break
							} else {
								// If this message is not committed back, it will be fetched by in next FetchMessage().
								// eventManager.Save() should handle this case when gets duplicate key error.
								glog.Errorf("storage: commit to kafka err: %v", err)
								if err == context.Canceled {
									glog.V(5).Info("storage: commit to kafka was cancelled")
									return
								}
								backoff(&sleep)
								select {
								case <-time.After(sleep):
									continue
								case <-ctx.Done():
									return
								}
							}
						}
						break
					} else {
						glog.Errorf("storage: save message to mysql err: %v", err)
						if err == context.Canceled {
							glog.V(5).Info("storage: save was cancelled")
							return
						}
						if s.es.IsDupKeyError(err) {
							panic(err) // NOTE: panic
						}
						backoff(&sleep)
						select {
						case <-time.After(sleep):
							continue
						case <-ctx.Done():
							return
						}
					}
				}
			}
		} else {
			glog.Errorf("storage: fetch from kafka err: %v", err)
			if err == context.Canceled /* || err == io.EOF || err == io.ErrUnexpectedEOF*/ {
				glog.V(5).Info("storage: fetch was cancelled")
				return
			}
			backoff(&sleep)
			select {
			case <-time.After(sleep):
				continue
			case <-ctx.Done():
				return
			}
		}
	}
}

func backoff(d *time.Duration) {
	if *d == 0 {
		*d = BackoffMinInterval
	} else {
		*d = time.Duration(float64(*d) * BackoffMultiplier)
		if *d < BackoffMaxInterval {
			*d = d.Truncate(time.Millisecond)
		} else {
			*d = BackoffMinInterval
		}
	}
}

func (s *storage) shouldDiscard(msg *kafka.Message) bool {
	return s.delEventsTtlDays > 0 && time.Since(msg.Time) > time.Duration(s.delEventsTtlDays)*24*time.Hour
}

func (s *storage) decodeKafkaMsg(msg *kafka.Message) *pb.E2EMsg {
	if len(msg.Value) > int(s.valueMaxBytes) {
		glog.Errorf("storage: kafka value out of limit, msg.Value: %s", string(msg.Value))
		return nil
	}
	var v pb.E2EMsg
	if err := json.Unmarshal(msg.Value, &v); err != nil {
		glog.Errorf("storage: failed to unmarshal kafka msg value: `%s`, error: %v", msg.Value, err)
		return nil
	}

	if s.shouldDiscard(msg) {
		glog.Errorf("storage: ignore incoming message because too old, msg.Offset: %d, msg.Time: %s", msg.Offset, msg.Time)
		return nil
	}

	return &v
}
