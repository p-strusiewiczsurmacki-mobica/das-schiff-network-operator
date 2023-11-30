/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ServiceReconciler reconciles a Service object.
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Reconciler *reconciler.Reconciler
}

const (
	prefix            = "network.schiff.telekom.de"
	ipAnnotation      = prefix + "/ipAddresses"
	loadBalancerClass = prefix + "/internal-lb"
)

//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;update;watch
//+kubebuilder:rbac:groups=network.schiff.telekom.de,resources=Services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.schiff.telekom.de,resources=Services/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.schiff.telekom.de,resources=Services/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// _ = log.FromContext(ctx)

	svc := corev1.Service{}
	if err := r.Client.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting service %s-%s: %w", req.Namespace, req.Name, err)
	}

	*svc.Spec.LoadBalancerClass = loadBalancerClass

	switch *svc.Spec.IPFamilyPolicy {
	case v1.IPFamilyPolicyRequireDualStack:
		//
	case v1.IPFamilyPolicyPreferDualStack:
		//
	default:
		//
	}

	svc.Annotations[ipAnnotation] = "192.168.1.1,ffff::"

	if err := r.Client.Update(ctx, &svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("error updating service %s-%s: %w", req.Namespace, req.Name, err)
	}

	return ctrl.Result{RequeueAfter: requeueTime}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager, withEndpoints bool) error {
	// Create empty request for changes to node

	predicates := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isLoadBalancerService(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isLoadBalancerService(e.ObjectNew) || isLoadBalancerService(e.ObjectOld)
		},
		DeleteFunc:  func(e event.DeleteEvent) bool { return isLoadBalancerService(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}

	controller := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}, builder.WithPredicates(predicates))

	logger := ctrl.Log.WithName("service-controller")

	if withEndpoints {

		epSlicesMapFn := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			epSlice, ok := obj.(*discovery.EndpointSlice)
			if !ok {
				logger.Info("received an object that is not epslice")
				return []reconcile.Request{}
			}
			serviceName, err := serviceKeyForSlice(epSlice)
			if err != nil {
				logger.Error(err, "failed to get serviceName for slice", "epslice", epSlice.Name)
				return []reconcile.Request{}
			}
			logger.Info("enqueueing", "service", serviceName, "epslice", epSlice)
			return []reconcile.Request{{NamespacedName: serviceName}}
		})

		epMapFn := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			endpoints, ok := obj.(*v1.Endpoints)
			if !ok {
				logger.Info("received an object that is not an endpoint")
				return []reconcile.Request{}
			}
			serviceName := types.NamespacedName{Name: endpoints.Name, Namespace: endpoints.Namespace}
			logger.Info("enqueueing", "service", serviceName, "endpoints", endpoints)
			return []reconcile.Request{{NamespacedName: serviceName}}
		})

		mapFn := epMapFn

		clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
		if err != nil {
			return fmt.Errorf("creating Kubernetes client: %s", err)
		}

		if useEndpointSlices(clientset) {
			mapFn = epSlicesMapFn
		}

		controller = controller.Watches(&corev1.Endpoints{}, mapFn)

	}

	err := controller.Complete(r)
	if err != nil {
		return fmt.Errorf("error creating controller: %w", err)
	}
	return nil
}

func isLoadBalancerService(object client.Object) bool {
	s, ok := object.(*corev1.Service)
	if !ok {
		return false
	}
	if s.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return false
	}

	return true
}

// useEndpointSlices detect if Endpoints Slices are enabled in the cluster.
func useEndpointSlices(kubeClient kubernetes.Interface) bool {
	if _, err := kubeClient.Discovery().ServerResourcesForGroupVersion(discovery.SchemeGroupVersion.String()); err != nil {
		return false
	}
	// this is needed to check if ep slices are enabled on the cluster. In 1.17 the resources are there but disabled by default
	if _, err := kubeClient.DiscoveryV1().EndpointSlices("default").Get(context.Background(), "kubernetes", metav1.GetOptions{}); err != nil {
		return false
	}
	return true
}

func serviceKeyForSlice(endpointSlice *discovery.EndpointSlice) (types.NamespacedName, error) {
	if endpointSlice == nil {
		return types.NamespacedName{}, fmt.Errorf("nil EndpointSlice")
	}
	serviceName, err := serviceNameForSlice(endpointSlice)
	if err != nil {
		return types.NamespacedName{}, err
	}

	return types.NamespacedName{Namespace: endpointSlice.Namespace, Name: serviceName}, nil
}

func serviceNameForSlice(endpointSlice *discovery.EndpointSlice) (string, error) {
	serviceName, ok := endpointSlice.Labels[discovery.LabelServiceName]
	if !ok || serviceName == "" {
		return "", fmt.Errorf("endpointSlice missing the %s label", discovery.LabelServiceName)
	}
	return serviceName, nil
}
