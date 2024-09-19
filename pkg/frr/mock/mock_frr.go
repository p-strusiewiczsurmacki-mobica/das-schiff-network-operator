// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/telekom/das-schiff-network-operator/pkg/frr (interfaces: ManagerInterface)

// Package mock_frr is a generated GoMock package.
package mock_frr

import (
	reflect "reflect"

	frr "github.com/telekom/das-schiff-network-operator/pkg/frr"
	nl "github.com/telekom/das-schiff-network-operator/pkg/nl"
	gomock "go.uber.org/mock/gomock"
)

// MockManagerInterface is a mock of ManagerInterface interface.
type MockManagerInterface struct {
	ctrl     *gomock.Controller
	recorder *MockManagerInterfaceMockRecorder
}

// MockManagerInterfaceMockRecorder is the mock recorder for MockManagerInterface.
type MockManagerInterfaceMockRecorder struct {
	mock *MockManagerInterface
}

// NewMockManagerInterface creates a new mock instance.
func NewMockManagerInterface(ctrl *gomock.Controller) *MockManagerInterface {
	mock := &MockManagerInterface{ctrl: ctrl}
	mock.recorder = &MockManagerInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockManagerInterface) EXPECT() *MockManagerInterfaceMockRecorder {
	return m.recorder
}

// Configure mocks base method.
func (m *MockManagerInterface) Configure(arg0 frr.Configuration, arg1 *nl.Manager) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Configure", arg0, arg1)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Configure indicates an expected call of Configure.
func (mr *MockManagerInterfaceMockRecorder) Configure(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Configure", reflect.TypeOf((*MockManagerInterface)(nil).Configure), arg0, arg1)
}

// GetStatusFRR mocks base method.
func (m *MockManagerInterface) GetStatusFRR() (string, string, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetStatusFRR")
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(string)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// GetStatusFRR indicates an expected call of GetStatusFRR.
func (mr *MockManagerInterfaceMockRecorder) GetStatusFRR() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetStatusFRR", reflect.TypeOf((*MockManagerInterface)(nil).GetStatusFRR))
}

// Init mocks base method.
func (m *MockManagerInterface) Init(arg0 string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Init", arg0)
	ret0, _ := ret[0].(error)
	return ret0
}

// Init indicates an expected call of Init.
func (mr *MockManagerInterfaceMockRecorder) Init(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Init", reflect.TypeOf((*MockManagerInterface)(nil).Init), arg0)
}

// ReloadFRR mocks base method.
func (m *MockManagerInterface) ReloadFRR() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ReloadFRR")
	ret0, _ := ret[0].(error)
	return ret0
}

// ReloadFRR indicates an expected call of ReloadFRR.
func (mr *MockManagerInterfaceMockRecorder) ReloadFRR() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ReloadFRR", reflect.TypeOf((*MockManagerInterface)(nil).ReloadFRR))
}

// RestartFRR mocks base method.
func (m *MockManagerInterface) RestartFRR() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RestartFRR")
	ret0, _ := ret[0].(error)
	return ret0
}

// RestartFRR indicates an expected call of RestartFRR.
func (mr *MockManagerInterfaceMockRecorder) RestartFRR() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RestartFRR", reflect.TypeOf((*MockManagerInterface)(nil).RestartFRR))
}

// SetConfigPath mocks base method.
func (m *MockManagerInterface) SetConfigPath(arg0 string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetConfigPath", arg0)
}

// SetConfigPath indicates an expected call of SetConfigPath.
func (mr *MockManagerInterfaceMockRecorder) SetConfigPath(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetConfigPath", reflect.TypeOf((*MockManagerInterface)(nil).SetConfigPath), arg0)
}