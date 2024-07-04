package reconciler

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t,
		"Reconciler Suite")
}

var _ = Describe("ConfigReconciler", func() {
	Context("NewConfigReconciler() should", func() {
		It("return new config reconciler", func() {
			c := createClient()
			r, err := NewConfigReconciler(c, logr.New(nil), time.Millisecond*100)
			Expect(r).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
		})
	})
	Context("CreateConfigForNode() should", func() {
		It("return config for provided node", func() {
			c := createClient()
			cmInfo := make(chan bool)
			defer close(cmInfo)
			r, err := NewConfigReconciler(c, logr.New(nil), time.Millisecond)
			Expect(err).ToNot(HaveOccurred())

			r.globalCfg = v1alpha1.NewEmptyConfig("global")
			r.globalCfg.Spec.Layer2 = []v1alpha1.Layer2NetworkConfigurationSpec{
				{
					NodeSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
				},
			}

			cfg, err := r.CreateConfigForNode("node", &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "node",
					Labels: map[string]string{"app": "test"},
				},
			})

			Expect(cfg).ToNot(BeNil())
			Expect(err).ToNot(HaveOccurred())
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
