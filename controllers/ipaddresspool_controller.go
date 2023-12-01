// /*
// Copyright 2022.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     http://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package controllers

// import (
// 	"context"
// 	"fmt"

// 	corev1 "k8s.io/api/core/v1"
// 	v1 "k8s.io/api/core/v1"
// 	discovery "k8s.io/api/discovery/v1"
// 	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
// 	networkv1alpha1 "github.com/telekom/das-schiff-network-operator/api/v1alpha1"
// 	"k8s.io/apimachinery/pkg/runtime"
// 	"k8s.io/apimachinery/pkg/types"
// 	"k8s.io/client-go/kubernetes"
// 	ctrl "sigs.k8s.io/controller-runtime"
// 	"sigs.k8s.io/controller-runtime/pkg/builder"
// 	"sigs.k8s.io/controller-runtime/pkg/client"
// 	"sigs.k8s.io/controller-runtime/pkg/event"
// 	"sigs.k8s.io/controller-runtime/pkg/handler"
// 	"sigs.k8s.io/controller-runtime/pkg/predicate"
// 	"sigs.k8s.io/controller-runtime/pkg/reconcile"
// )

// // IPAddressPoolReconciler reconciles a IPAddressPool object.
// type IPAddressPoolReconciler struct {
// 	client.Client
// 	Scheme *runtime.Scheme

// 	// Reconciler *reconciler.Reconciler
// }

//+kubebuilder:rbac:groups=network.schiff.telekom.de,resources=ipaddresspools,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.schiff.telekom.de,resources=ipaddresspools/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.schiff.telekom.de,resources=ipaddresspools/finalizers,verbs=update

// // Reconcile is part of the main kubernetes reconciliation loop which aims to
// // move the current state of the cluster closer to the desired state.
// //
// // For more details, check Reconcile and its Result here:
// // - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
// func (r *IPAddressPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
// 	// _ = log.FromContext(ctx)

// 	svc := networkv1alpha1.IPAddressPool{}
// 	if err := r.Client.Get(ctx, req.NamespacedName, &svc); err != nil {
// 		return ctrl.Result{}, fmt.Errorf("error getting IPAddressPool %s-%s: %w", req.Namespace, req.Name, err)
// 	}

// 	return ctrl.Result{RequeueAfter: requeueTime}, nil
// }

// // SetupWithManager sets up the controller with the Manager.
// func (r *IPAddressPoolReconciler) SetupWithManager(mgr ctrl.Manager, withEndpoints bool) error {
// 	// Create empty request for changes to node

// 	predicates := predicate.Funcs{
// 		CreateFunc: func(e event.CreateEvent) bool {
// 			return isLoadBalancerIPAddressPool(e.Object)
// 		},
// 		UpdateFunc: func(e event.UpdateEvent) bool {
// 			return isLoadBalancerIPAddressPool(e.ObjectNew) || isLoadBalancerIPAddressPool(e.ObjectOld)
// 		},
// 		DeleteFunc:  func(e event.DeleteEvent) bool { return isLoadBalancerIPAddressPool(e.Object) },
// 		GenericFunc: func(e event.GenericEvent) bool { return false },
// 	}

// 	controller := ctrl.NewControllerManagedBy(mgr).
// 		For(&networkv1alpha1.IPAddressPool{}, builder.WithPredicates(predicates))

// 	logger := ctrl.Log.WithName("IPAddressPool-controller")

// 	if withEndpoints {

// 		epSlicesMapFn := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
// 			epSlice, ok := obj.(*discovery.EndpointSlice)
// 			if !ok {
// 				logger.Info("received an object that is not epslice")
// 				return []reconcile.Request{}
// 			}
// 			IPAddressPoolName, err := IPAddressPoolKeyForSlice(epSlice)
// 			if err != nil {
// 				logger.Error(err, "failed to get IPAddressPoolName for slice", "epslice", epSlice.Name)
// 				return []reconcile.Request{}
// 			}
// 			logger.Info("enqueueing", "IPAddressPool", IPAddressPoolName, "epslice", epSlice)
// 			return []reconcile.Request{{NamespacedName: IPAddressPoolName}}
// 		})

// 		epMapFn := handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
// 			endpoints, ok := obj.(*v1.Endpoints)
// 			if !ok {
// 				logger.Info("received an object that is not an endpoint")
// 				return []reconcile.Request{}
// 			}
// 			IPAddressPoolName := types.NamespacedName{Name: endpoints.Name, Namespace: endpoints.Namespace}
// 			logger.Info("enqueueing", "IPAddressPool", IPAddressPoolName, "endpoints", endpoints)
// 			return []reconcile.Request{{NamespacedName: IPAddressPoolName}}
// 		})

// 		mapFn := epMapFn

// 		clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
// 		if err != nil {
// 			return fmt.Errorf("creating Kubernetes client: %s", err)
// 		}

// 		if useEndpointSlices(clientset) {
// 			mapFn = epSlicesMapFn
// 		}

// 		controller = controller.Watches(&corev1.Endpoints{}, mapFn)

// 	}

// 	err := controller.Complete(r)
// 	if err != nil {
// 		return fmt.Errorf("error creating controller: %w", err)
// 	}
// 	return nil
// }

// func isLoadBalancerIPAddressPool(object client.Object) bool {
// 	s, ok := object.(*corev1.IPAddressPool)
// 	if !ok {
// 		return false
// 	}
// 	if s.Spec.Type != corev1.IPAddressPoolTypeLoadBalancer {
// 		return false
// 	}

// 	return true
// }

// // useEndpointSlices detect if Endpoints Slices are enabled in the cluster.
// func useEndpointSlices(kubeClient kubernetes.Interface) bool {
// 	if _, err := kubeClient.Discovery().ServerResourcesForGroupVersion(discovery.SchemeGroupVersion.String()); err != nil {
// 		return false
// 	}
// 	// this is needed to check if ep slices are enabled on the cluster. In 1.17 the resources are there but disabled by default
// 	if _, err := kubeClient.DiscoveryV1().EndpointSlices("default").Get(context.Background(), "kubernetes", metav1.GetOptions{}); err != nil {
// 		return false
// 	}
// 	return true
// }

// func IPAddressPoolKeyForSlice(endpointSlice *discovery.EndpointSlice) (types.NamespacedName, error) {
// 	if endpointSlice == nil {
// 		return types.NamespacedName{}, fmt.Errorf("nil EndpointSlice")
// 	}
// 	IPAddressPoolName, err := IPAddressPoolNameForSlice(endpointSlice)
// 	if err != nil {
// 		return types.NamespacedName{}, err
// 	}

// 	return types.NamespacedName{Namespace: endpointSlice.Namespace, Name: IPAddressPoolName}, nil
// }

// func IPAddressPoolNameForSlice(endpointSlice *discovery.EndpointSlice) (string, error) {
// 	IPAddressPoolName, ok := endpointSlice.Labels[discovery.LabelIPAddressPoolName]
// 	if !ok || IPAddressPoolName == "" {
// 		return "", fmt.Errorf("endpointSlice missing the %s label", discovery.LabelIPAddressPoolName)
// 	}
// 	return IPAddressPoolName, nil
// }
