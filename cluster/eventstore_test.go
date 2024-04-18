package cluster

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/segmentio/kafka-go"

	cluster_mock "github.com/mqy/minipush/cluster/mock"
	"github.com/mqy/minipush/store"
	store_mock "github.com/mqy/minipush/store/mock"
)

func TestConsumeLoop(t *testing.T) {
	// vscode settings.json
	// "go.testFlags": ["-v", "-count=1", "-timeout=15s", "-test.v", "-args", "-v=5", "-logtostderr"],
	flag.Parse()

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	storeMock := store_mock.NewMockIEventStore(mockCtrl)
	kafkaMock := cluster_mock.NewMockIKafkaReader(mockCtrl)

	s := newStorage(storeMock, kafkaMock, nil, true, 30, 1024)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var offset int64

	kafkaMock.EXPECT().Close().Times(1)

	kafkaMock.EXPECT().FetchMessage(ctx).DoAndReturn(func(context.Context) (kafka.Message, error) {
		t.Logf("mock FetchMessage")
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			t.Logf("mock FetchMessage: ctx done, err: %v", ctx.Err())
			return kafka.Message{}, ctx.Err()
		}

		if false { // flip this switch to see the output
			offset++
			return kafka.Message{
				Offset: offset,
				Key:    []byte(fmt.Sprintf("%d", offset)),
				Value:  []byte(`{"body":"a","uids":[1,2]}`),
				Time:   time.Now(),
			}, nil
		} else {
			return kafka.Message{}, errors.New("some error")
		}
	}).AnyTimes()

	storeMock.EXPECT().Save(ctx, gomock.Any()).DoAndReturn(func(context.Context, *store.Event) (map[int32]int32, error) {
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			t.Logf("mock Save: ctx done, err: %v", ctx.Err())
			return nil, ctx.Err()
		}

		if true { // flip this switch to see the output
			return map[int32]int32{1: 1, 2: 2}, nil
		} else {
			return nil, errors.New("some error")
		}
	}).AnyTimes()

	kafkaMock.EXPECT().CommitMessages(ctx, gomock.Any()).DoAndReturn(func(context.Context, ...kafka.Message) error {
		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			t.Logf("mock CommitMessages: ctx done, err: %v", ctx.Err())
			return ctx.Err()
		}

		if true { // flip this switch to see the output
			return nil
		} else {
			return errors.New("some error")
		}
	}).AnyTimes()

	go s.consumeLoop(ctx)

	time.Sleep(5 * time.Second)
	t.Logf("call cancel()")
	cancel()
	s.wg.Wait()
	t.Logf("storage exit")
}
