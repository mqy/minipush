package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

const (
	dsn = "root:@tcp(127.0.0.1:3306)/minipush?parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci"
)

func TestIncSeq(t *testing.T) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}

	for _, table := range []string{"events", "user_events", "user_seq"} {
		if _, err := db.Exec("DELETE FROM " + table); err != nil {
			panic(err)
		}
	}

	s := NewEventStore(db)

	uids := []int32{1, 2, 3}

	var lock sync.Mutex
	result := make(map[int32]int32)

	var wg sync.WaitGroup
	const N = 50
	for j := 0; j < N; j++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for k := 0; k < 3; k++ {
				uid := uids[k]
				err := s.withTx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
					seq, err := s.incSeq(context.Background(), tx, uids[k])
					if err != nil {
						return err
					}

					lock.Lock()
					if v, ok := result[uid]; ok {
						if seq > v {
							result[uid] = seq
						} else if seq == v {
							return fmt.Errorf("same seq")
						}
					} else {
						result[uid] = seq
					}
					lock.Unlock()
					return nil
				}, &sql.TxOptions{Isolation: sql.LevelSerializable})
				if err != nil {
					panic(err)
				}
			}
		}(j)
	}

	wg.Wait()

	for _, uid := range uids {
		if result[uid] != N {
			panic("unexpected seq")
		}
	}
}
