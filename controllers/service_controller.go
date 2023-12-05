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
	prefix             = "network.schiff.telekom.de"
	ipAnnotation       = prefix + "/ipAddresses"
	poolAnnotation     = prefix + "/pool"
	loadBalancerClass  = prefix + "/internal-lb"
	ipFamilyAnnotation = prefix + "/ipFamily"
	ipv4Family         = "ipv4"
	ipv6Family         = "ipv6"
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

	// find requested service
	svc := corev1.Service{}
	if err := r.Client.Get(ctx, req.NamespacedName, &svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting service %s-%s: %w", req.Namespace, req.Name, err)
	}

	// get target pool from the service's annotation
	targetPool, ok := svc.Annotations[poolAnnotation]
	if !ok {
		return ctrl.Result{}, fmt.Errorf("error getting pool for service %s-%s", req.Namespace, req.Name)
	}

	// get pool from the cluster
	pool := networkv1alpha1.IPAddressPool{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: "", Name: targetPool}, &pool); err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting pool %s for service %s-%s: %w", targetPool, req.Namespace, req.Name, err)
	}

	// list all services - this will be used to find addresses frpom the pool that were already used
	allSvc := corev1.ServiceList{}
	if err := r.Client.List(ctx, &allSvc, &client.ListOptions{}); err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting services: %w", err)
	}

	// check if pool is IPv4, IPv6 or DualStack
	isIPv4Pool := pool.Spec.IPv4 != ""
	isIPv6Pool := pool.Spec.IPv6 != ""

	// check if request is IPv4, IPv6 or DualStack
	requestIPv4 := strings.Contains(svc.Annotations[ipFamilyAnnotation], ipv4Family)
	requestIPv6 := strings.Contains(svc.Annotations[ipFamilyAnnotation], ipv6Family)

	// if request's IP family is not supported by the pool return error
	if !isIPv4Pool && requestIPv4 {
		return ctrl.Result{}, fmt.Errorf("cannot request IPv4 from non-IPv4 pool")
	}

	if !isIPv6Pool && requestIPv6 {
		return ctrl.Result{}, fmt.Errorf("cannot request IPv6 from non-IPv6 pool")
	}

	// we create IPSets for IPv4 and IPv6 networks of the pools (if supported and requested)
	// iSets is a map that can be referenced as ipSets[ipFamily][full/free] to get full network or only free addresses
	ipSets := map[string]map[string]*netipx.IPSet{}
	var err error

	if isIPv4Pool && requestIPv4 {
		if ipSets[ipv4Family], err = getAddrSets(pool.Spec.IPv4, allSvc.Items); err != nil {
			return ctrl.Result{}, err
		}
	}

	if isIPv6Pool && requestIPv6 {
		if ipSets[ipv6Family], err = getAddrSets(pool.Spec.IPv6, allSvc.Items); err != nil {
			return ctrl.Result{}, err
		}
	}

	// check if service was not already processes and contains IP address from the pool
	// if so, we should not process the request (?)
	if svc.Annotations[ipAnnotation] != "" && len(svc.Spec.ExternalIPs) > 0 {
		for _, ip := range svc.Spec.ExternalIPs {
			exIP, err := netip.ParseAddr(ip)
			if err != nil {
				return ctrl.Result{}, err
			}
			if ipSets[ipv4Family]["full"].Contains(exIP) || ipSets[ipv6Family]["full"].Contains(exIP) {
				return ctrl.Result{}, nil
			}
		}
	}

	// find addresses that can be used by the service
	// this might be later a part of separate IPAM component
	ipFamilies := strings.Split(svc.Annotations[ipAnnotation], ",")

	var ips []*netip.Addr
	for _, ipFamily := range ipFamilies {
		switch ipFamily {
		case ipv4Family:
			ips = append(ips, getFirstAddress(ipSets[ipv4Family]["free"]))
		case ipv6Family:
			ips = append(ips, getFirstAddress(ipSets[ipv6Family]["free"]))
		default:
			logger.Error(fmt.Errorf("invalid family"), "%s is nat a valid IP Address family - please use ipv4 or ipv6", ipFamily)
		}
	}

	// create annotation
	annotation := ""
	// externalIPs := []string{}
	for i, ip := range ips {
		s := ip.String()
		annotation += s
		if i < (len(ips) - 1) {
			annotation += ","
		}
	}

	// set annotation and update the service
	svc.Annotations[ipAnnotation] = annotation
	if err := r.Client.Update(ctx, &svc); err != nil {
		return ctrl.Result{}, fmt.Errorf("error updating service %s-%s: %w", req.Namespace, req.Name, err)
	}

	logger.Info("created service with annotation", "namespace", svc.Namespace, "name", svc.Name, "annotation", svc.Annotations[ipAnnotation])

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
	// as cidr was passed to ipSet should be safe to assume that only 1 range was created in ipSet
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

func getAddrSets(cidr string, services []v1.Service) (map[string]*netipx.IPSet, error) {
	sets := map[string]*netipx.IPSet{}

	if set, err := fullSetFromCidr(cidr); err != nil {
		return nil, err
	} else {
		sets["full"] = set
	}

	if set, err := findFreeAddresses(cidr, services); err != nil {
		return nil, err
	} else {
		sets["free"] = set
	}

	return sets, nil
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
