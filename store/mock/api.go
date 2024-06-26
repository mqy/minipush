// Code generated by MockGen. DO NOT EDIT.
// Source: store/api.go

// Package mock is a generated GoMock package.
package mock

import (
	context "context"
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"

	proto "github.com/mqy/minipush/proto"
	store "github.com/mqy/minipush/store"
)

// MockIEventStore is a mock of IEventStore interface.
type MockIEventStore struct {
	ctrl     *gomock.Controller
	recorder *MockIEventStoreMockRecorder
}

// MockIEventStoreMockRecorder is the mock recorder for MockIEventStore.
type MockIEventStoreMockRecorder struct {
	mock *MockIEventStore
}

// NewMockIEventStore creates a new mock instance.
func NewMockIEventStore(ctrl *gomock.Controller) *MockIEventStore {
	mock := &MockIEventStore{ctrl: ctrl}
	mock.recorder = &MockIEventStoreMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockIEventStore) EXPECT() *MockIEventStoreMockRecorder {
	return m.recorder
}

// DeleteOutdatedEvents mocks base method.
func (m *MockIEventStore) DeleteOutdatedEvents(ctx context.Context, ttlDays int32) (int32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeleteOutdatedEvents", ctx, ttlDays)
	ret0, _ := ret[0].(int32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// DeleteOutdatedEvents indicates an expected call of DeleteOutdatedEvents.
func (mr *MockIEventStoreMockRecorder) DeleteOutdatedEvents(ctx, ttlDays interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeleteOutdatedEvents", reflect.TypeOf((*MockIEventStore)(nil).DeleteOutdatedEvents), ctx, ttlDays)
}

// GetEvents mocks base method.
func (m *MockIEventStore) GetEvents(ctx context.Context, uid int32, req *proto.GetEventsReq) (*proto.GetEventsResp, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetEvents", ctx, uid, req)
	ret0, _ := ret[0].(*proto.GetEventsResp)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetEvents indicates an expected call of GetEvents.
func (mr *MockIEventStoreMockRecorder) GetEvents(ctx, uid, req interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetEvents", reflect.TypeOf((*MockIEventStore)(nil).GetEvents), ctx, uid, req)
}

// GetReadStates mocks base method.
func (m *MockIEventStore) GetReadStates(ctx context.Context, uid, ttlDays int32, req *proto.GetReadStatesReq) (*proto.GetReadStatesResp, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetReadStates", ctx, uid, ttlDays, req)
	ret0, _ := ret[0].(*proto.GetReadStatesResp)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetReadStates indicates an expected call of GetReadStates.
func (mr *MockIEventStoreMockRecorder) GetReadStates(ctx, uid, ttlDays, req interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetReadStates", reflect.TypeOf((*MockIEventStore)(nil).GetReadStates), ctx, uid, ttlDays, req)
}

// IsDupKeyError mocks base method.
func (m *MockIEventStore) IsDupKeyError(err error) bool {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsDupKeyError", err)
	ret0, _ := ret[0].(bool)
	return ret0
}

// IsDupKeyError indicates an expected call of IsDupKeyError.
func (mr *MockIEventStoreMockRecorder) IsDupKeyError(err interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsDupKeyError", reflect.TypeOf((*MockIEventStore)(nil).IsDupKeyError), err)
}

// Save mocks base method.
func (m *MockIEventStore) Save(ctx context.Context, e *store.Event) (map[int32]int32, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Save", ctx, e)
	ret0, _ := ret[0].(map[int32]int32)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Save indicates an expected call of Save.
func (mr *MockIEventStoreMockRecorder) Save(ctx, e interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Save", reflect.TypeOf((*MockIEventStore)(nil).Save), ctx, e)
}

// SetRead mocks base method.
func (m *MockIEventStore) SetRead(ctx context.Context, uid, seq int32) (*proto.SetReadResp, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SetRead", ctx, uid, seq)
	ret0, _ := ret[0].(*proto.SetReadResp)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SetRead indicates an expected call of SetRead.
func (mr *MockIEventStoreMockRecorder) SetRead(ctx, uid, seq interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetRead", reflect.TypeOf((*MockIEventStore)(nil).SetRead), ctx, uid, seq)
}

// Stats mocks base method.
func (m *MockIEventStore) Stats(ctx context.Context, uid, ttlDays int32, req *proto.StatsReq) (*proto.StatsResp, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Stats", ctx, uid, ttlDays, req)
	ret0, _ := ret[0].(*proto.StatsResp)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Stats indicates an expected call of Stats.
func (mr *MockIEventStoreMockRecorder) Stats(ctx, uid, ttlDays, req interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Stats", reflect.TypeOf((*MockIEventStore)(nil).Stats), ctx, uid, ttlDays, req)
}
