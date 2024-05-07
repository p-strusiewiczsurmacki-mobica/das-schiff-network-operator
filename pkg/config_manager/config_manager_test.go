package configmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/nodeconfig"
	mock_reconciler "github.com/telekom/das-schiff-network-operator/pkg/reconciler/mock"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
	nodeName := "testNode"
	Context("WatchConfigs() should", func() {
		It("return no error to errCh if context was cancelled", func() {
			cm, _, _ := prepareObjects()
			defer close(cm.changes)
			defer close(cm.deletedNodes)

			ctx, cancel := context.WithCancel(context.TODO())
			err := runContextTest(ctx, cm, cancel)
			Expect(err).ToNot(HaveOccurred())
		})
		It("return error to errCh if context is done for reason other cancelation", func() {
			cm, _, _ := prepareObjects()
			defer close(cm.changes)
			defer close(cm.deletedNodes)

			ctx, cancel := context.WithTimeout(context.TODO(), time.Millisecond*100)
			defer cancel()

			err := runContextTest(ctx, cm, nil)
			Expect(err).To(HaveOccurred())
		})
		It("return error to errCh if cannot create config for node", func() {
			cm, crm, nrm := prepareObjects()
			defer close(cm.changes)
			defer close(cm.deletedNodes)

			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			cm.configsMap.Store(nodeName, "invalid")
			nrm.EXPECT().GetNodes().Return(map[string]*corev1.Node{nodeName: {}})
			crm.EXPECT().CreateConfigForNode(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("fake error"))

			errCh := make(chan error)
			defer close(errCh)
			err := runTest(ctx, cm, errCh, nil)
			Expect(err).To(HaveOccurred())
		})
		It("return no error to errCh if config for node already exists in-memory (cofig update)", func() {
			cm, crm, nrm := prepareObjects()
			defer close(cm.changes)
			defer close(cm.deletedNodes)

			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			nrm.EXPECT().GetNodes().Return(map[string]*corev1.Node{nodeName: {ObjectMeta: metav1.ObjectMeta{Name: nodeName}}})
			cm.configsMap.Store(nodeName, nodeconfig.NewEmpty(nodeName))
			crm.EXPECT().CreateConfigForNode(gomock.Any(), gomock.Any()).Return(&v1alpha1.NodeConfig{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}, nil)

			errCh := make(chan error)
			defer close(errCh)
			err := runTest(ctx, cm, errCh, cancel)
			Expect(err).ToNot(HaveOccurred())
		})
		It("return no error to errCh if config for node already exists in-memory (cofig update)", func() {
			cm, crm, nrm := prepareObjects()
			defer close(cm.changes)
			defer close(cm.deletedNodes)

			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			nrm.EXPECT().GetNodes().Return(map[string]*corev1.Node{nodeName: {ObjectMeta: metav1.ObjectMeta{Name: nodeName}}})
			crm.EXPECT().CreateConfigForNode(gomock.Any(), gomock.Any()).Return(&v1alpha1.NodeConfig{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}, nil)

			errCh := make(chan error)
			defer close(errCh)
			err := runTest(ctx, cm, errCh, cancel)
			Expect(err).ToNot(HaveOccurred())
		})
		It("return no error if can create/update NodeConfigProcess and the NodeConfigs objects", func() {
			cm, crm, nrm := prepareObjects()
			defer close(cm.changes)
			defer close(cm.deletedNodes)

			ctx, cancel := context.WithCancel(context.TODO())
			defer cancel()

			nrm.EXPECT().GetNodes().Return(map[string]*corev1.Node{nodeName: {ObjectMeta: metav1.ObjectMeta{Name: nodeName}}})
			crm.EXPECT().CreateConfigForNode(gomock.Any(), gomock.Any()).Return(&v1alpha1.NodeConfig{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}, nil)

			errCh := make(chan error)
			defer close(errCh)
			err := runTest(ctx, cm, errCh, cancel)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func prepareObjects() (*ConfigManager, *mock_reconciler.MockConfigReconcilerInterface, *mock_reconciler.MockNodeReconcilerInterface) {
	crm := mock_reconciler.NewMockConfigReconcilerInterface(ctrl)
	nrm := mock_reconciler.NewMockNodeReconcilerInterface(ctrl)

	s := runtime.NewScheme()
	err := corev1.AddToScheme(s)
	Expect(err).ToNot(HaveOccurred())
	err = v1alpha1.AddToScheme(s)
	Expect(err).ToNot(HaveOccurred())
	c := fake.NewClientBuilder().WithScheme(s).Build()

	changes := make(chan bool)
	nodesDeleted := make(chan []string)
	cm := New(c, crm, nrm, logr.New(nil), time.Second*10, -1, changes, nodesDeleted)
	Expect(cm).ToNot(BeNil())
	return cm, crm, nrm
}

func runTest(ctx context.Context, cm *ConfigManager, errCh chan error, cancel context.CancelFunc) error {
	start := make(chan bool)
	defer close(start)
	go func() {
		start <- true
		cm.WatchConfigs(ctx, errCh)
	}()
	startVal := <-start
	Expect(startVal).To(BeTrue())

	time.Sleep(time.Millisecond * 100)
	cm.changes <- true
	time.Sleep(time.Millisecond * 100)
	if cancel != nil {
		cancel()
	}
	err := <-errCh
	return err
}

func runContextTest(ctx context.Context, cm *ConfigManager, cancel context.CancelFunc) error {
	errCh := make(chan error)
	defer close(errCh)
	quit := make(chan bool)
	defer close(quit)
	go func() {
		cm.WatchConfigs(ctx, errCh)
		quit <- true
	}()
	if cancel != nil {
		cancel()
	}
	err := <-errCh
	<-quit
	return err
}
