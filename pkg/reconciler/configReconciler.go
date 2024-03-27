package reconciler

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"github.com/telekom/das-schiff-network-operator/pkg/debounce"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	statusProvisioning = "provisioning"
)

type ConfigReconciler struct {
	client    client.Client
	logger    logr.Logger
	debouncer *debounce.Debouncer
}

type reconcileConfig struct {
	*ConfigReconciler
	logr.Logger
}

func (cr *ConfigReconciler) Reconcile(ctx context.Context) {
	cr.debouncer.Debounce(ctx)
}

func NewConfigReconciler(clusterClient client.Client, logger logr.Logger) (*ConfigReconciler, error) {
	reconciler := &ConfigReconciler{
		client: clusterClient,
		logger: logger,
	}

	reconciler.debouncer = debounce.NewDebouncer(reconciler.reconcileDebounced, defaultDebounceTime, logger)

	return reconciler, nil
}

func (cr *ConfigReconciler) reconcileDebounced(ctx context.Context) error {
	cr.logger.Info("ConfigReconciler - reconcileDebounced")
	r := &reconcileConfig{
		ConfigReconciler: cr,
		Logger:           cr.logger,
	}

	cr.logger.Info("ConfigReconciler - fetchLayer3")
	l3vnis, err := r.fetchLayer3(ctx)
	if err != nil {
		return err
	}

	cr.logger.Info("ConfigReconciler - fetchLayer2")
	l2vnis, err := r.fetchLayer2(ctx)
	if err != nil {
		return err
	}

	cr.logger.Info("ConfigReconciler - fetchTass")
	taas, err := r.fetchTaas(ctx)
	if err != nil {
		return err
	}

	l2Spec := []v1alpha1.Layer2NetworkConfigurationSpec{}
	for _, v := range l2vnis {
		l2Spec = append(l2Spec, v.Spec)
	}

	l3Spec := []v1alpha1.VRFRouteConfigurationSpec{}
	for _, v := range l3vnis {
		l3Spec = append(l3Spec, v.Spec)
	}

	taasSpec := []v1alpha1.RoutingTableSpec{}
	for _, v := range taas {
		taasSpec = append(taasSpec, v.Spec)
	}

	cr.logger.Info("ConfigReconciler - list nodes")
	nodes, err := cr.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	cr.logger.Info("ConfigReconciler - list configs")
	existingConfigs, err := cr.ListConfigs(ctx)
	if err != nil {
		return fmt.Errorf("error listing configs: %w", err)
	}

	newConfigs := map[string]*v1alpha1.NodeConfig{}

	for name := range *nodes {
		var c *v1alpha1.NodeConfig
		if _, exists := (*existingConfigs)[name]; exists {
			cr.logger.Info("ConfigReconciler - config exists")
			x := (*existingConfigs)[name]
			c = &x
			// c.Spec.Layer2 = l2Spec
			c.Spec.Vrf = l3Spec
			c.Spec.RoutingTable = taasSpec

			// err = cr.client.Update(ctx, c)
			// if err != nil {
			// 	cr.logger.Error(err, "error updateing NodeConfig object")
			// 	return err
			// }
		} else {
			cr.logger.Info("ConfigReconciler - new config")
			c = &v1alpha1.NodeConfig{
				ObjectMeta: v1.ObjectMeta{
					Name: name,
				},
				Spec: v1alpha1.NodeConfigSpec{
					// Layer2:       l2Spec,
					Vrf:          l3Spec,
					RoutingTable: taasSpec,
				}}
			// err = cr.client.Create(ctx, c)
			// if err != nil {
			// 	cr.logger.Error(err, "error creating NodeConfig object")
			// 	return err
			// }
		}
		newConfigs[name] = c
	}

	cr.logger.Info("ConfigReconciler - process layer 2 configs")
	newL2Config := make(map[string][]*v1alpha1.Layer2NetworkConfigurationSpec)

	for i := range l2Spec {
		cr.logger.Info("l2spec", "spec", l2Spec[i])
		if l2Spec[i].NodeSelector != nil {
			cr.logger.Info("Node selector is not nil")
			selector := labels.NewSelector()
			var reqs labels.Requirements

			for key, value := range l2Spec[i].NodeSelector.MatchLabels {
				requirement, err := labels.NewRequirement(key, selection.Equals, []string{value})
				if err != nil {
					cr.logger.Error(err, "error creating MatchLabel requirement")
					return fmt.Errorf("error creating MatchLabel requirement: %w", err)
				}
				reqs = append(reqs, *requirement)
			}

			for _, req := range l2Spec[i].NodeSelector.MatchExpressions {
				lowercaseOperator := selection.Operator(strings.ToLower(string(req.Operator)))
				requirement, err := labels.NewRequirement(req.Key, lowercaseOperator, req.Values)
				if err != nil {
					cr.logger.Error(err, "error creating MatchExpression requirement")
					return fmt.Errorf("error creating MatchExpression requirement: %w", err)
				}
				reqs = append(reqs, *requirement)
			}
			selector = selector.Add(reqs...)

			cr.logger.Info("converted selector", "selector", selector)

			for name := range *nodes {
				if selector.Matches(labels.Set((*nodes)[name].ObjectMeta.Labels)) {
					cr.logger.Info("node does match nodeSelector of layer2", "node", name)
					newL2Config[name] = append(newL2Config[name], &l2Spec[i])
				}
			}

		} else {
			cr.logger.Info("IS NIL")
			for name := range *nodes {
				newL2Config[name] = append(newL2Config[name], &l2Spec[i])
			}
		}
	}

	cr.logger.Info("new L2 config", "map", newL2Config)

	for name := range *nodes {
		newConfigs[name].Spec.Layer2 = []v1alpha1.Layer2NetworkConfigurationSpec{}
		for _, v := range newL2Config[name] {
			newConfigs[name].Spec.Layer2 = append(newConfigs[name].Spec.Layer2, *v)
		}
	}

	cr.logger.Info("ConfigReconciler - API query")
	for name := range newConfigs {
		cr.logger.Info("NodeConfig", name, *newConfigs[name])
		if _, exists := (*existingConfigs)[name]; exists {
			cr.logger.Info("ConfigReconciler - API query - update")
			err = cr.client.Update(ctx, newConfigs[name])
			if err != nil {
				cr.logger.Error(err, "error updating NodeConfig object")
				return err
			}
		} else {
			cr.logger.Info("ConfigReconciler - API query - create")
			err = cr.client.Create(ctx, newConfigs[name])
			if err != nil {
				cr.logger.Error(err, "error creating NodeConfig object")
				return err
			}
		}
		err = cr.client.Status().Update(ctx, newConfigs[name])
		if err != nil {
			cr.logger.Error(err, "error creating NodeConfig status")
			return err
		}

		instance := &v1alpha1.NodeConfig{}
		err := cr.client.Get(ctx, types.NamespacedName{Name: newConfigs[name].Name, Namespace: newConfigs[name].Namespace}, instance)
		if err != nil {
			cr.logger.Error(err, "error getting current instance of config")
			return err
		}

		instance.Status.ConfigStatus = statusProvisioning
		err = cr.client.Status().Update(ctx, instance)
		if err != nil {
			cr.logger.Error(err, "error creating NodeConfig status")
			return err
		}
	}

	cr.logger.Info("ConfigReconciler - success")
	return nil
}

func (cr *ConfigReconciler) ListNodes(ctx context.Context) (*map[string]corev1.Node, error) {
	list := &corev1.NodeList{}

	if err := cr.client.List(ctx, list); err != nil {
		return nil, err
	}

	nodes := make(map[string]corev1.Node)

	for i := range list.Items {
		if _, exists := list.Items[i].Labels["node-role.kubernetes.io/control-plane"]; !exists {
			nodes[list.Items[i].Name] = list.Items[i]
		}
	}

	return &nodes, nil
}

func (cr *ConfigReconciler) ListConfigs(ctx context.Context) (*map[string]v1alpha1.NodeConfig, error) {
	list := &v1alpha1.NodeConfigList{}
	if err := cr.client.List(ctx, list); err != nil {
		return nil, err
	}

	configs := make(map[string]v1alpha1.NodeConfig)

	for i := range list.Items {
		configs[list.Items[i].Name] = list.Items[i]
	}

	return &configs, nil
}
