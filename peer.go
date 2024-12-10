package k8speer

import (
	"context"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

var addressTypes []discoveryv1.AddressType = []discoveryv1.AddressType{
	discoveryv1.AddressTypeIPv4,
	discoveryv1.AddressTypeIPv6,
	discoveryv1.AddressTypeFQDN,
}

type Peer struct {
	ID  string
	URI string
}

func WatchPeers(
	ctx context.Context,
	ll *slog.Logger,
	k8s *kubernetes.Clientset,
	serviceName string,
	namespace string,
	port string,
	onPeer func(peers []Peer),
) error {
	// prime the cache
	k8s.DiscoveryV1().EndpointSlices(namespace).Watch(ctx, metav1.ListOptions{})

	// start watching for change
	handler := &esWatcher{
		ctx:         ctx,
		ll:          ll,
		serviceName: serviceName,
		port:        port,
		onPeer:      onPeer,
		peers:       make(map[discoveryv1.AddressType]map[string][]Peer),
	}

	i := informers.NewSharedInformerFactoryWithOptions(
		k8s,
		time.Minute,
		informers.WithNamespace(namespace),
	)
	i.Discovery().V1().EndpointSlices().Informer().AddEventHandler(handler)
	i.Start(ctx.Done())
	<-ctx.Done()
	i.Shutdown()
	return ctx.Err()
}

type esWatcher struct {
	ctx         context.Context
	ll          *slog.Logger
	serviceName string
	port        string
	onPeer      func(peers []Peer)
	mu          sync.Mutex
	peers       map[discoveryv1.AddressType]map[string][]Peer
}

func (w *esWatcher) isRelevant(es *discoveryv1.EndpointSlice) bool {
	sn, ok := es.Labels[discoveryv1.LabelServiceName]
	return ok && sn == w.serviceName
}

func (w *esWatcher) handleUpdate(es *discoveryv1.EndpointSlice) {
	if !w.isRelevant(es) {
		return
	}

	isActive := func(ep discoveryv1.Endpoint) bool {
		if ep.Conditions.Terminating != nil && *ep.Conditions.Terminating {
			return false
		}
		if ep.Conditions.Ready == nil && !(*ep.Conditions.Ready) {
			return false
		}
		return len(ep.Addresses) > 0
	}

	epaddrs := make([]Peer, 0, len(es.Endpoints))
	for _, ep := range es.Endpoints {
		if !isActive(ep) {
			continue
		}
		epaddrs = append(epaddrs, Peer{
			ID:  net.JoinHostPort(ep.Addresses[0], w.port),
			URI: net.JoinHostPort(ep.Addresses[0], w.port),
		})
	}
	w.ll.DebugContext(w.ctx, "endpoint slice updated", slog.String("name", es.Name))

	w.mu.Lock()
	defer w.mu.Unlock()
	ess, ok := w.peers[es.AddressType]
	if !ok {
		ess = make(map[string][]Peer)
		w.peers[es.AddressType] = ess
	}
	ess[es.Name] = epaddrs
	w.update()
}

func (w *esWatcher) update() {
	var peers []Peer
	for _, at := range addressTypes {
		m, ok := w.peers[at]
		if !ok {
			continue
		}
		for _, ps := range m {
			peers = append(peers, ps...)
		}
	}
	slices.SortFunc(peers, func(a, b Peer) int {
		return strings.Compare(a.ID, b.ID)
	})
	w.onPeer(peers)
}

func (w *esWatcher) onDelete(es *discoveryv1.EndpointSlice) {
	if !w.isRelevant(es) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ll.DebugContext(w.ctx, "endpoint slice deleted", slog.String("", es.Name))
	m, ok := w.peers[es.AddressType]
	if !ok {
		return
	}
	delete(m, es.Name)
	w.update()
}

func (w *esWatcher) OnAdd(obj interface{}, isInInitialList bool) {
	es, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return
	}
	w.handleUpdate(es)
}
func (w *esWatcher) OnUpdate(_, newObj interface{}) {
	newes, ok := newObj.(*discoveryv1.EndpointSlice)
	if !ok {
		return
	}
	w.handleUpdate(newes)
}
func (w *esWatcher) OnDelete(obj interface{}) {
	es, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return
	}
	w.onDelete(es)
}

