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
	"net"
	"net/netip"
	"strings"

	networkv1alpha1 "github.com/telekom/das-schiff-network-operator/api/v1alpha1"
	"go4.org/netipx"
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
	"sigs.k8s.io/controller-runtime/pkg/log"
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
	poolAnnotation    = prefix + "/pool"
	loadBalancerClass = prefix + "/internal-lb"
)

//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;update;watch
//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=services/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Context", "ctx", ctx)

	svc := corev1.Service{}
	if err := r.Client.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting service %s-%s: %w", req.Namespace, req.Name, err)
	}

	// logger.Info("Service found")

	targetPool, ok := svc.Annotations[poolAnnotation]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("error getting pool for service %s-%s", req.Namespace, req.Name)
	}

	// logger.Info("Target pool: ", "targetPool", targetPool)

	pool := networkv1alpha1.IPAddressPool{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: "", Name: targetPool}, &pool); err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting pool %s for service %s-%s: %w", targetPool, req.Namespace, req.Name, err)
	}

	// logger.Info("Found pool: ", "pool", pool)

	allSvc := corev1.ServiceList{}
	if err := r.Client.List(ctx, &allSvc, &client.ListOptions{}); err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting services: %w", err)
	}

	// logger.Info("found services")

	isIPv4Pool := pool.Spec.IPv4 != ""
	isIPv6Pool := pool.Spec.IPv6 != ""

	ipv4Pool := &netipx.IPSet{}
	ipv6Pool := &netipx.IPSet{}
	ipv4Full := &netipx.IPSet{}
	ipv6Full := &netipx.IPSet{}
	var err error

	if isIPv4Pool {
		// logger.Info("will find ipv4 ipset")
		if ipv4Full, err = fullSetFromCidr(pool.Spec.IPv4); err != nil {
			return ctrl.Result{}, err
		}

		if ipv4Pool, err = findFreeAddresses(pool.Spec.IPv4, allSvc.Items); err != nil {
			return ctrl.Result{}, err
		}
	}

	if isIPv6Pool {
		// logger.Info("will find ipv6 ipset")
		if ipv6Full, err = fullSetFromCidr(pool.Spec.IPv6); err != nil {
			return ctrl.Result{}, err
		}
		if ipv6Pool, err = findFreeAddresses(pool.Spec.IPv6, allSvc.Items); err != nil {
			return ctrl.Result{}, err
		}
	}

	if svc.Annotations[ipAnnotation] != "" && len(svc.Spec.ExternalIPs) > 0 {
		logger.Info("Probably aready processed")
		for _, ip := range svc.Spec.ExternalIPs {
			logger.Info("Checking IP", "ip", ip)
			exIP, err := netip.ParseAddr(ip)
			if err != nil {
				logger.Info("unable to parse address")
				return ctrl.Result{}, err
			}
			if ipv4Full.Contains(exIP) || ipv6Full.Contains(exIP) {
				logger.Info("already processed -return")
				return ctrl.Result{}, nil
			}
		}
	}

	annotation := ""

	var ipv4, ipv6 *netip.Addr

	// it seems that's not what the IPFamilyPolicy field is for. Need to change that.
	switch *svc.Spec.IPFamilyPolicy {
	case v1.IPFamilyPolicyRequireDualStack:
		if !isIPv4Pool || !isIPv6Pool {
			return ctrl.Result{}, fmt.Errorf("error updating service %s-%s: referenced pool is not a dualstack pool", req.Namespace, req.Name)
		}
		ipv4 = getFirstAddress(ipv4Pool)
		ipv6 = getFirstAddress(ipv6Pool)

		annotation = ipv4.String() + "," + ipv6.String()
	case v1.IPFamilyPolicyPreferDualStack:
		if isIPv4Pool {
			ipv4 = getFirstAddress(ipv4Pool)
			annotation = ipv4.String()
		}
		if annotation != "" {
			annotation += ","
		}
		if isIPv6Pool {
			ipv6 = getFirstAddress(ipv6Pool)
			annotation += ipv6.String()
		}
	default:
		// should get cluster default family or spec.ipFamilies
		if svc.Spec.IPFamilies[0] == corev1.IPv4Protocol {
			if isIPv4Pool {
				ipv4 = getFirstAddress(ipv4Pool)
				annotation = ipv4.String()
				break
			}
			return ctrl.Result{}, fmt.Errorf("cannot assign IPv4 address from non-IPv4 pool")
		}
		if svc.Spec.IPFamilies[0] == corev1.IPv4Protocol {
			if isIPv6Pool {
				ipv6 = getFirstAddress(ipv6Pool)
				annotation = ipv6.String()
				break
			}
			return ctrl.Result{}, fmt.Errorf("cannot assign IPv6 address from non-IPv6 pool")
		}
	}

	logger.Info("will apply annotation", "annotation", annotation)
	svc.Annotations[ipAnnotation] = annotation

	// logger.Info("will set loadbalancer class")

	// class := loadBalancerClass
	// svc.Spec.LoadBalancerClass = &class

	ips := []string{}
	if ipv4 != nil {
		ips = append(ips, ipv4.String())
	}

	if ipv6 != nil {
		ips = append(ips, ipv6.String())
	}

	logger.Info("Applying external IPs", "ips", ips)

	svc.Spec.ExternalIPs = ips

	logger.Info("will update service")
	if err := r.Client.Update(ctx, &svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("error updating service %s-%s: %w", req.Namespace, req.Name, err)
	}

	logger.Info("all OK")
	return ctrl.Result{}, nil
}

func buildSetFromCidr(address string) (*netipx.IPSetBuilder, error) {
	builder := &netipx.IPSetBuilder{}
	prefix, err := netip.ParsePrefix(address)
	if err != nil {
		return nil, err
	}
	builder.AddPrefix(prefix)
	return builder, nil
}

func fullSetFromCidr(cidr string) (*netipx.IPSet, error) {
	builder, err := buildSetFromCidr(cidr)
	if err != nil {
		return nil, err
	}
	return builder.IPSet()
}

func findFreeAddresses(cidr string, services []v1.Service) (*netipx.IPSet, error) {
	addr := strings.Split(cidr, "/")
	parsed := net.ParseIP(addr[0])

	if parsed == nil {
		return nil, fmt.Errorf("unable to parse cidr: %s", cidr)
	}

	isCidrIPv4 := parsed.To4() != nil

	builder, err := buildSetFromCidr(cidr)
	if err != nil {
		return nil, err
	}
	ipPool, err := builder.IPSet()
	if err != nil {
		return nil, err
	}

	// remove first and last address in network
	// as cidr was passed to  it should be safe to assume that only 1 range was created in ipSet
	builder.Remove(ipPool.Ranges()[0].From())
	builder.Remove(ipPool.Ranges()[0].To())

	ipPool, err = builder.IPSet()
	if err != nil {
		return nil, err
	}

	for i := range services {
		svcIPs := strings.Split(services[i].Annotations[ipAnnotation], ",")
		for _, svcIP := range svcIPs {
			if svcIP != "" {
				addr := net.ParseIP(svcIP)
				if addr == nil {
					continue
				}

				isIPv4 := addr.To4() != nil

				if isIPv4 == isCidrIPv4 {
					ip, err := netip.ParseAddr(svcIP)
					if err != nil {
						return nil, fmt.Errorf("error parsing IP %s: %w", svcIP, err)
					}
					if ipPool.Contains(ip) {
						builder.Remove(ip)
					}
				}
			}
		}
	}

	return builder.IPSet()
}

func getFirstAddress(ipset *netipx.IPSet) *netip.Addr {
	ip := ipset.Ranges()[0].From()
	return &ip
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager, withEndpoints bool) error {
	// Create empty request for changes to node

	predicates := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isLoadBalancerService(e.Object) && e.Object.GetAnnotations()[ipAnnotation] == ""
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return (isLoadBalancerService(e.ObjectNew) || isLoadBalancerService(e.ObjectOld))
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

// functions stolen from metallb/kube-vip
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
