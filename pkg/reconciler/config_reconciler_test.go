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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	processStateJSON = `
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
					"state": ""
				}
			}
		],
		"kind": "List",
		"metadata": {
			"resourceVersion": ""
		}
	}	
	`

	nodeConfigsJSON = `
	{
		"apiVersion": "v1",
		"items": [
			{
				"apiVersion": "network.schiff.telekom.de/v1alpha1",
				"kind": "NodeConfig",
				"metadata": {
					"name": "kind-worker"
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
					"name": "kind-worker-backup"
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

	nodeConfigsInvalidJSON = `
	{
		"apiVersion": "v1",
		"items": [
			{
				"apiVersion": "network.schiff.telekom.de/v1alpha1",
				"kind": "NodeConfig",
				"metadata": {
					"name": "kind-worker-invalid"
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

	nodesJSON = `{
		"apiVersion": "v1",
		"items": [
			{
				"apiVersion": "v1",
				"kind": "Node",
				"metadata": {
					"name": "kind-worker"
				}
			}
		],
		"kind": "List",
		"metadata": {
			"resourceVersion": ""
		}
	}
	`

	fakeProcess           *v1alpha1.NodeConfigProcessList
	fakeNodeConfig        *v1alpha1.NodeConfigList
	fakeNodeConfigInvalid *v1alpha1.NodeConfigList
	fakeNodes             *corev1.NodeList
	tmpPath               string
	ctrl                  *gomock.Controller
	s                     *runtime.Scheme
)

var _ = BeforeSuite(func() {
	fakeProcess = &v1alpha1.NodeConfigProcessList{}
	err := json.Unmarshal([]byte(processStateJSON), fakeProcess)
	Expect(err).ShouldNot(HaveOccurred())
	fakeNodeConfig = &v1alpha1.NodeConfigList{}
	err = json.Unmarshal([]byte(nodeConfigsJSON), fakeNodeConfig)
	Expect(err).ShouldNot(HaveOccurred())
	fakeNodeConfigInvalid = &v1alpha1.NodeConfigList{}
	err = json.Unmarshal([]byte(nodeConfigsInvalidJSON), fakeNodeConfigInvalid)
	Expect(err).ShouldNot(HaveOccurred())
	fakeNodes = &corev1.NodeList{}
	err = json.Unmarshal([]byte(nodesJSON), fakeNodes)
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
	err = corev1.AddToScheme(s)
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
			fakeProcess.Items[0].Spec.State = statusProvisioning
			c := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(fakeProcess, fakeNodeConfig).Build()
			r, err := NewConfigReconciler(c, logr.Logger{}, DefaultTimeout, 1)
			Expect(err).ToNot(HaveOccurred())
			err = r.ValidateFormerLeader(context.TODO())
			Expect(err).ToNot(HaveOccurred())
		})
		It("return error if cannot update status subresource", func() {
			fakeProcess.Items[0].Spec.State = statusProvisioning
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
			fakeProcess.Items[0].Spec.State = statusProvisioning
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
			fakeProcess.Items[0].Spec.State = statusProvisioning
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
			quit := make(chan bool)
			wg.Add(1)
			go setStatus(ctx, c, &fakeNodeConfig.Items[0], statusProvisioning, statusInvalid, &wg, errCh, quit)
			err = r.ValidateFormerLeader(ctx)
			Expect(err).To(HaveOccurred())
			quit <- true
			wg.Wait()
			Expect(errCh).To(BeEmpty())
		})
		It("return no error if operator sets config's status to "+statusProvisioned, func() {
			fakeProcess.Items[0].Spec.State = statusProvisioning
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
			quit := make(chan bool)
			wg.Add(1)
			go setStatus(ctx, c, &fakeNodeConfig.Items[0], statusProvisioning, statusProvisioned, &wg, errCh, quit)
			err = r.ValidateFormerLeader(ctx)
			Expect(err).ToNot(HaveOccurred())
			quit <- true
			wg.Wait()
			Expect(errCh).To(BeEmpty())
		})
	})
	Context("reconcileDebounced() should", func() {
		It("return no error if node was provisioned", func() {
			err := testReconciler(statusProvisioned)
			Expect(err).ToNot(HaveOccurred())
		})
		It("return error if node reported invalid state", func() {
			err := testReconciler(statusInvalid)
			Expect(err).To(HaveOccurred())
		})
		It("return error if new config equals known invalid config", func() {
			fakeProcess.Items[0].Spec.State = statusProvisioned
			c := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(fakeProcess, fakeNodeConfig, fakeNodeConfigInvalid, fakeNodes).
				WithStatusSubresource(&fakeNodeConfig.Items[0]).
				Build()
			r, err := NewConfigReconciler(c, logr.Logger{}, "1s", 1)
			Expect(err).ToNot(HaveOccurred())

			ctx := context.TODO()

			err = r.ValidateFormerLeader(ctx)
			Expect(err).ToNot(HaveOccurred())

			wg := sync.WaitGroup{}

			wg.Add(1)

			go func() {
				defer wg.Done()
				err = r.reconcileDebounced(ctx)
			}()

			r.OnLeaderElectionDone <- true
			close(r.OnLeaderElectionDone)

			wg.Wait()
			Expect(err).To(HaveOccurred())
		})
	})
})

func setStatus(ctx context.Context, c client.Client, o *v1alpha1.NodeConfig, oldValue, newValue string, wg *sync.WaitGroup, errCh chan error, quit chan bool) {
	for {
		select {
		case <-quit:
			wg.Done()
			return
		default:
			tmp := &v1alpha1.NodeConfig{}
			if err := c.Get(ctx, client.ObjectKeyFromObject(o), tmp); err != nil && !apierrors.IsNotFound(err) {
				errCh <- err
			}
			if tmp.Status.ConfigStatus == oldValue {
				tmp.Status.ConfigStatus = newValue
				if err := c.Status().Update(ctx, tmp); err != nil {
					errCh <- err
				}
			}
		}
	}
}

func testReconciler(expectedStatus string) error {
	fakeProcess.Items[0].Spec.State = statusProvisioned
	c := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(fakeProcess, fakeNodeConfig, fakeNodes).
		WithStatusSubresource(&fakeNodeConfig.Items[0]).
		Build()
	r, err := NewConfigReconciler(c, logr.Logger{}, "1s", 1)
	Expect(err).ToNot(HaveOccurred())

	ctx := context.TODO()

	err = r.ValidateFormerLeader(ctx)
	Expect(err).ToNot(HaveOccurred())

	quit := make(chan bool)

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = r.reconcileDebounced(ctx)
		quit <- true
	}()

	r.OnLeaderElectionDone <- true
	close(r.OnLeaderElectionDone)

	errCh := make(chan error)

	wg.Add(1)
	go setStatus(ctx, c, &fakeNodeConfig.Items[0], statusProvisioning, expectedStatus, &wg, errCh, quit)

	wg.Wait()
	close(errCh)

	return err
}
