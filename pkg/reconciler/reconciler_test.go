package reconciler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t,
		"Reconciler Suite")
}

var (
	fakeNodesJSON = `{"items":[{"metadata":{"name":"node"}}]}`
	fakeNodes     *corev1.NodeList
)

var _ = BeforeSuite(func() {
	fakeNodes = &corev1.NodeList{}
	err := json.Unmarshal([]byte(fakeNodesJSON), fakeNodes)
	Expect(err).ShouldNot(HaveOccurred())
})

var _ = Describe("ConfigReconciler", func() {
	Context("NewConfigReconciler() should", func() {
		It("return new config reconciler", func() {
			c := createClient()
			cmInfo := make(chan bool)
			r, err := NewConfigReconciler(c, logr.New(nil), time.Millisecond*100, cmInfo)
			Expect(r).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
	})
	Context("reconcileDebounced() should", func() {
		It("return no error if fetched data succesfuly", func() {
			c := createClient()
			cmInfo := make(chan bool)
			defer close(cmInfo)
			r, err := NewConfigReconciler(c, logr.New(nil), time.Millisecond, cmInfo)
			Expect(r).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
			ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
			defer cancel()
			// var err error
			go func() {
				err = r.reconcileDebounced(ctx)
			}()

			<-cmInfo
			Expect(err).ToNot(HaveOccurred())
		})
	})
	Context("CreateConfigForNode() should", func() {
		It("return config for provided node", func() {
			c := createClient()
			cmInfo := make(chan bool)
			defer close(cmInfo)
			r, err := NewConfigReconciler(c, logr.New(nil), time.Millisecond, cmInfo)
			Expect(err).ToNot(HaveOccurred())

			r.globalCfg = v1alpha1.NewEmptyConfig("global")
			r.globalCfg.Spec.Layer2 = []v1alpha1.Layer2NetworkConfigurationSpec{
				{
					NodeSelector: &v1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				},
			}

			cfg, err := r.CreateConfigForNode("node", &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name:   "node",
					Labels: map[string]string{"app": "test"},
				},
			})

			Expect(cfg).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("NodeReconciler", func() {
	Context("reconcileDebounced() should", func() {
		It("return no error and inform about new nodes added and deleted", func() {
			n := &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			c := createClient(n)
			cmInfo := make(chan bool)
			defer close(cmInfo)
			nodeDelInfo := make(chan []string)
			defer close(nodeDelInfo)

			r, err := NewNodeReconciler(c, logr.New(nil), time.Second, cmInfo, nodeDelInfo)
			Expect(r).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			go func() {
				err = r.reconcileDebounced(ctx)
			}()
			info := <-cmInfo
			Expect(info).To(BeTrue())
			Expect(err).ToNot(HaveOccurred())

			err = c.Delete(context.Background(), n)
			Expect(err).ToNot(HaveOccurred())

			go func() {
				err = r.reconcileDebounced(ctx)
			}()
			deleted := <-nodeDelInfo
			Expect(deleted).To(HaveLen(1))
		})
	})
	Context("GetNodes() should", func() {
		It("return a map of currently known nodes", func() {
			n := &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			c := createClient(n)
			cmInfo := make(chan bool)
			defer close(cmInfo)
			nodeDelInfo := make(chan []string)
			defer close(nodeDelInfo)

			r, err := NewNodeReconciler(c, logr.New(nil), time.Second, cmInfo, nodeDelInfo)
			Expect(r).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			go func() {
				err = r.reconcileDebounced(ctx)
			}()
			info := <-cmInfo
			Expect(info).To(BeTrue())
			Expect(err).ToNot(HaveOccurred())

			nodes := r.GetNodes()
			Expect(nodes).To(HaveLen(1))
		})
	})
})

func createClient(initObjs ...runtime.Object) client.Client {
	s := runtime.NewScheme()
	err := corev1.AddToScheme(s)
	Expect(err).ToNot(HaveOccurred())
	err = v1alpha1.AddToScheme(s)
	Expect(err).ToNot(HaveOccurred())
	return fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(initObjs...).Build()
}
