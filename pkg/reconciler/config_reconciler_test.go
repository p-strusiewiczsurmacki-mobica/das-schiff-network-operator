package reconciler

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"go.uber.org/mock/gomock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	fakeProcessState = `
	{
		"apiVersion": "v1",
		"items": [
			{
				"apiVersion": "network.schiff.telekom.de/v1alpha1",
				"kind": "NodeConfigProcess",
				"metadata": {
					"creationTimestamp": "2024-04-15T11:19:06Z",
					"generation": 12,
					"name": "network-operator",
					"resourceVersion": "223252",
					"uid": "4ad359bb-bb7d-4a1d-bf43-551c04b592d5"
				},
				"spec": {
					"state": "provisioning"
				}
			}
		],
		"kind": "List",
		"metadata": {
			"resourceVersion": ""
		}
	}	
	`

	emptyNodeConfig = `
	{
		"apiVersion": "v1",
		"items": [
			{
				"apiVersion": "network.schiff.telekom.de/v1alpha1",
				"kind": "NodeConfig",
				"metadata": {
					"creationTimestamp": "2024-04-15T11:22:08Z",
					"generation": 2,
					"name": "kind-worker",
					"resourceVersion": "222987",
					"uid": "fc0376a2-7f6a-4388-8166-298b21cf2f89"
				},
				"spec": {
					"layer2": [],
					"routingTable": [],
					"vrf": []
				},
				"status": {
					"configStatus": "provisioned"
				}
			},
			{
				"apiVersion": "network.schiff.telekom.de/v1alpha1",
				"kind": "NodeConfig",
				"metadata": {
					"creationTimestamp": "2024-04-15T11:22:08Z",
					"generation": 3,
					"name": "kind-worker-backup",
					"resourceVersion": "223106",
					"uid": "5b0ed728-47ed-46cb-a678-8e32dda826ee"
				},
				"spec": {
					"layer2": [],
					"routingTable": [],
					"vrf": []
				}
			}
		],
		"kind": "List",
		"metadata": {
			"resourceVersion": ""
		}
	}
	`

	fakeProcess    *v1alpha1.NodeConfigProcessList
	fakeNodeConfig *v1alpha1.NodeConfigList
	tmpPath        string
	ctrl           *gomock.Controller
	s              *runtime.Scheme
)

var _ = BeforeSuite(func() {
	fakeProcess = &v1alpha1.NodeConfigProcessList{}
	err := json.Unmarshal([]byte(fakeProcessState), fakeProcess)
	Expect(err).ShouldNot(HaveOccurred())
	fakeNodeConfig = &v1alpha1.NodeConfigList{}
	err = json.Unmarshal([]byte(emptyNodeConfig), fakeNodeConfig)
	Expect(err).ShouldNot(HaveOccurred())
})

func TestConfigReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	tmpPath = t.TempDir()
	ctrl = gomock.NewController(t)
	defer ctrl.Finish()
	s = runtime.NewScheme()
	err := v1alpha1.AddToScheme(s)
	Expect(err).ToNot(HaveOccurred())
	RunSpecs(t,
		"ConfigReconciler Suite")
}

var _ = Describe("HealthCheck", func() {
	Context("NewConfigReconciler() should", func() {
		c := fake.NewClientBuilder().Build()
		It("return error if cannot parse time duration", func() {
			_, err := NewConfigReconciler(c, logr.Logger{}, "invalidTimeout", 1)
			Expect(err).To(HaveOccurred())
		})
		It("return error if invalid limit is provided", func() {
			_, err := NewConfigReconciler(c, logr.Logger{}, DefaultTimeout, -1)
			Expect(err).To(HaveOccurred())
		})
		It("return no error", func() {
			_, err := NewConfigReconciler(c, logr.Logger{}, DefaultTimeout, 1)
			Expect(err).ToNot(HaveOccurred())
		})
	})
	Context("ValidateFormerLeader() should", func() {
		It("return no error if process status does not yet exist", func() {
			c := fake.NewClientBuilder().WithScheme(s).Build()
			r, err := NewConfigReconciler(c, logr.Logger{}, DefaultTimeout, 1)
			Expect(err).ToNot(HaveOccurred())
			err = r.ValidateFormerLeader(context.TODO())
			Expect(err).ToNot(HaveOccurred())
		})
		It("return no error if backup config is equal to current config", func() {
			c := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(fakeProcess, fakeNodeConfig).Build()
			r, err := NewConfigReconciler(c, logr.Logger{}, DefaultTimeout, 1)
			Expect(err).ToNot(HaveOccurred())
			err = r.ValidateFormerLeader(context.TODO())
			Expect(err).ToNot(HaveOccurred())
		})
		It("return error if cannot update status subresource", func() {
			fakeNodeConfig.Items[0].Spec.RoutingTable = []v1alpha1.RoutingTableSpec{
				{
					TableID: 1,
				},
			}
			c := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(fakeProcess, fakeNodeConfig).
				Build()
			r, err := NewConfigReconciler(c, logr.Logger{}, DefaultTimeout, 1)
			Expect(err).ToNot(HaveOccurred())
			err = r.ValidateFormerLeader(context.TODO())
			Expect(err).To(HaveOccurred())
		})
		It("return error if request times out", func() {
			fakeNodeConfig.Items[0].Spec.RoutingTable = []v1alpha1.RoutingTableSpec{
				{
					TableID: 1,
				},
			}
			c := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(fakeProcess, fakeNodeConfig).
				WithStatusSubresource(&fakeNodeConfig.Items[0]).
				Build()
			// set 1ms timeout
			r, err := NewConfigReconciler(c, logr.Logger{}, "1ms", 1)
			Expect(err).ToNot(HaveOccurred())
			err = r.ValidateFormerLeader(context.TODO())
			Expect(err).To(HaveOccurred())
		})
		It("return error if operator sets config's status to "+statusInvalid, func() {
			fakeNodeConfig.Items[0].Spec.RoutingTable = []v1alpha1.RoutingTableSpec{
				{
					TableID: 1,
				},
			}
			c := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(fakeProcess, fakeNodeConfig).
				WithStatusSubresource(&fakeNodeConfig.Items[0]).
				Build()
			r, err := NewConfigReconciler(c, logr.Logger{}, DefaultTimeout, 1)
			Expect(err).ToNot(HaveOccurred())
			ctx := context.TODO()
			wg := sync.WaitGroup{}
			errCh := make(chan error)
			wg.Add(1)
			go setStatus(ctx, c, &fakeNodeConfig.Items[0], statusProvisioning, statusInvalid, &wg, errCh)
			err = r.ValidateFormerLeader(ctx)
			Expect(err).To(HaveOccurred())
			wg.Wait()
			Expect(errCh).To(BeEmpty())
		})
		It("return no error if operator sets config's status to "+statusProvisioned, func() {
			fakeNodeConfig.Items[0].Spec.RoutingTable = []v1alpha1.RoutingTableSpec{
				{
					TableID: 1,
				},
			}
			c := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(fakeProcess, fakeNodeConfig).
				WithStatusSubresource(&fakeNodeConfig.Items[0]).
				Build()
			r, err := NewConfigReconciler(c, logr.Logger{}, DefaultTimeout, 1)
			Expect(err).ToNot(HaveOccurred())
			ctx := context.TODO()
			wg := sync.WaitGroup{}
			errCh := make(chan error)
			wg.Add(1)
			go setStatus(ctx, c, &fakeNodeConfig.Items[0], statusProvisioning, statusProvisioned, &wg, errCh)
			err = r.ValidateFormerLeader(ctx)
			Expect(err).ToNot(HaveOccurred())
			wg.Wait()
			Expect(errCh).To(BeEmpty())
		})
	})
})

func setStatus(ctx context.Context, c client.Client, o *v1alpha1.NodeConfig, oldValue, newValue string, wg *sync.WaitGroup, errCh chan error) {
	for {
		tmp := &v1alpha1.NodeConfig{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(o), tmp); err != nil && !apierrors.IsNotFound(err) {
			errCh <- err
			return
		}
		if tmp.Status.ConfigStatus == oldValue {
			tmp.Status.ConfigStatus = newValue
			if err := c.Status().Update(ctx, tmp); err != nil {
				errCh <- err
				return
			}
			wg.Done()
			return
		}
	}
}
