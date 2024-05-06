package configmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	mock_reconciler "github.com/telekom/das-schiff-network-operator/pkg/reconciler/mock"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	ctrl *gomock.Controller
)

func TestConfigMap(t *testing.T) {
	RegisterFailHandler(Fail)
	ctrl = gomock.NewController(t)
	defer ctrl.Finish()
	RunSpecs(t,
		"ConfigManager Suite")
}

var _ = Describe("ConfigMap", func() {
	Context("WatchConfigs() should", func() {
		It("return no error to errCh if context was cancelled", func() {
			crm := mock_reconciler.NewMockConfigReconcilerInterface(ctrl)
			nrm := mock_reconciler.NewMockNodeReconcilerInterface(ctrl)
			c := fake.NewClientBuilder().Build()
			changes := make(chan bool)
			defer close(changes)
			nodesDeleted := make(chan []string)
			defer close(nodesDeleted)
			cm := New(c, crm, nrm, logr.New(nil), time.Second*10, -1, changes, nodesDeleted)
			Expect(cm).ToNot(BeNil())

			ctx, cancel := context.WithCancel(context.TODO())
			errCh := make(chan error)
			quit := make(chan bool)
			go func() {
				cm.WatchConfigs(ctx, errCh)
				quit <- true
				defer close(quit)
			}()
			cancel()
			err := <-errCh
			Expect(err).ToNot(HaveOccurred())
			<-quit
		})
		It("return error to errCh if context is done for reason other cancel", func() {
			crm := mock_reconciler.NewMockConfigReconcilerInterface(ctrl)
			nrm := mock_reconciler.NewMockNodeReconcilerInterface(ctrl)
			c := fake.NewClientBuilder().Build()
			changes := make(chan bool)
			defer close(changes)
			nodesDeleted := make(chan []string)
			defer close(nodesDeleted)
			cm := New(c, crm, nrm, logr.New(nil), time.Second*10, -1, changes, nodesDeleted)
			Expect(cm).ToNot(BeNil())

			ctx, cancel := context.WithTimeout(context.TODO(), time.Millisecond*100)
			defer cancel()
			errCh := make(chan error)
			quit := make(chan bool)
			go func() {
				cm.WatchConfigs(ctx, errCh)
				quit <- true
				defer close(quit)
			}()
			err := <-errCh
			Expect(err).To(HaveOccurred())
			<-quit
		})
		It("", func() {
			crm := mock_reconciler.NewMockConfigReconcilerInterface(ctrl)
			nrm := mock_reconciler.NewMockNodeReconcilerInterface(ctrl)
			c := fake.NewClientBuilder().Build()
			changes := make(chan bool)
			nodesDeleted := make(chan []string)
			cm := New(c, crm, nrm, logr.New(nil), time.Second*10, -1, changes, nodesDeleted)
			Expect(cm).ToNot(BeNil())
			defer close(changes)
			defer close(nodesDeleted)

			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()
			errCh := make(chan error)
			start := make(chan bool)
			cm.configsMap.Store("node", "invalid")
			nrm.EXPECT().GetNodes().Return(map[string]*corev1.Node{"node": {}})
			crm.EXPECT().CreateConfigForNode(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("fake error"))

			go func() {
				start <- true
				cm.WatchConfigs(ctx, errCh)
			}()
			startVal := <-start
			Expect(startVal).To(BeTrue())

			time.Sleep(time.Millisecond * 100)
			changes <- true
			time.Sleep(time.Millisecond * 100)
			err := <-errCh
			Expect(err).To(HaveOccurred())
		})
	})
})
