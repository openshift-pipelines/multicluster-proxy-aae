package registry

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	"github.com/openshift-pipelines/multicluster-proxy-aae/internal/config"
	tektonclient "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	kueuev1beta1 "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	kueueclient "sigs.k8s.io/kueue/client-go/clientset/versioned"
	kueueclientv1beta1 "sigs.k8s.io/kueue/client-go/clientset/versioned/typed/kueue/v1beta1"
)

const workerKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: worker
  cluster:
    server: https://worker.example.com
contexts:
- name: worker
  context:
    cluster: worker
    user: worker
current-context: worker
users:
- name: worker
  user:
    token: test
`

var errNotServed = errors.New("the server could not find the requested resource")

// fakeClusters stands in for the MultiKueueCluster API. It implements List and
// Watch, which is all the registry uses.
type fakeClusters struct {
	kueueclientv1beta1.MultiKueueClusterInterface

	mu sync.Mutex
	// unserved means the API does not exist, as before Kueue is installed.
	unserved bool
	names    []string
	lists    int
	watcher  *watch.FakeWatcher
}

func (f *fakeClusters) List(context.Context, metav1.ListOptions) (*kueuev1beta1.MultiKueueClusterList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists++
	if f.unserved {
		return nil, errNotServed
	}
	list := &kueuev1beta1.MultiKueueClusterList{}
	for _, name := range f.names {
		list.Items = append(list.Items, *multiKueueCluster(name))
	}
	return list, nil
}

func (f *fakeClusters) Watch(context.Context, metav1.ListOptions) (watch.Interface, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unserved {
		return nil, errNotServed
	}
	f.watcher = watch.NewFake()
	return f.watcher, nil
}

func (f *fakeClusters) serve(names ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unserved = false
	f.names = names
}

func (f *fakeClusters) listCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

func (f *fakeClusters) currentWatcher() *watch.FakeWatcher {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watcher
}

type fakeKueue struct {
	kueueclient.Interface
	clusters *fakeClusters
}

func (f fakeKueue) KueueV1beta1() kueueclientv1beta1.KueueV1beta1Interface {
	return fakeKueueV1beta1{clusters: f.clusters}
}

type fakeKueueV1beta1 struct {
	kueueclientv1beta1.KueueV1beta1Interface
	clusters *fakeClusters
}

func (f fakeKueueV1beta1) MultiKueueClusters() kueueclientv1beta1.MultiKueueClusterInterface {
	return f.clusters
}

// fakeKube serves a kubeconfig Secret for every worker.
type fakeKube struct{ kubernetes.Interface }

func (fakeKube) CoreV1() corev1client.CoreV1Interface { return fakeCoreV1{} }

type fakeCoreV1 struct{ corev1client.CoreV1Interface }

func (fakeCoreV1) Secrets(string) corev1client.SecretInterface { return fakeSecrets{} }

type fakeSecrets struct{ corev1client.SecretInterface }

func (fakeSecrets) Get(context.Context, string, metav1.GetOptions) (*corev1.Secret, error) {
	return &corev1.Secret{Data: map[string][]byte{"kubeconfig": []byte(workerKubeconfig)}}, nil
}

func multiKueueCluster(name string) *kueuev1beta1.MultiKueueCluster {
	return &kueuev1beta1.MultiKueueCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kueuev1beta1.MultiKueueClusterSpec{
			KubeConfig: kueuev1beta1.KubeConfig{
				Location:     name + "-kubeconfig",
				LocationType: kueuev1beta1.SecretLocationType,
			},
		},
	}
}

// startRegistry runs a registry against the fakes until the test ends.
func startRegistry(t *testing.T, clusters *fakeClusters) *WorkerConfigRegistry {
	t.Helper()
	r := &WorkerConfigRegistry{
		kubeClient:         fakeKube{},
		kueueClient:        fakeKueue{clusters: clusters},
		config:             &config.Config{WorkersSecretNamespace: "kueue-system"},
		configs:            make(map[string]*rest.Config),
		kubeClients:        make(map[string]kubernetes.Interface),
		tektonClients:      make(map[string]tektonclient.Interface),
		watchRetryInterval: 10 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		r.watchMultiKueueClusters(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	return r
}

// eventually fails the test unless cond becomes true in time.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// hasClusters reports whether the registry knows exactly these clusters. Pass
// them in sorted order.
func hasClusters(r *WorkerConfigRegistry, want ...string) func() bool {
	return func() bool {
		got := r.ListClusters()
		slices.Sort(got)
		return slices.Equal(got, want)
	}
}

// The proxy can start before Kueue is installed. It must find its workers once
// the MultiKueueCluster API is served.
func TestWorkerConfigRegistry_WaitsForMultiKueueClusterAPI(t *testing.T) {
	clusters := &fakeClusters{unserved: true}
	r := startRegistry(t, clusters)

	eventually(t, "the registry to retry", func() bool { return clusters.listCalls() >= 3 })
	if got := r.ListClusters(); len(got) != 0 {
		t.Fatalf("expected no clusters while the API is not served, got %v", got)
	}

	clusters.serve("worker-1")
	eventually(t, "worker-1 to be loaded", hasClusters(r, "worker-1"))
}

// The API server ends every watch after a while. Changes made while the
// registry is not watching must not be lost.
func TestWorkerConfigRegistry_ResumesAfterWatchEnds(t *testing.T) {
	clusters := &fakeClusters{names: []string{"worker-1", "worker-2"}}
	r := startRegistry(t, clusters)

	eventually(t, "the first watch", func() bool { return clusters.currentWatcher() != nil })
	first := clusters.currentWatcher()
	if !hasClusters(r, "worker-1", "worker-2")() {
		t.Fatalf("expected worker-1 and worker-2, got %v", r.ListClusters())
	}

	// worker-3 replaces worker-2 without an event, as if between two watches.
	clusters.serve("worker-1", "worker-3")
	first.Stop()

	eventually(t, "worker-3 to replace worker-2", hasClusters(r, "worker-1", "worker-3"))
	eventually(t, "a new watch", func() bool { return clusters.currentWatcher() != first })
}

// While the watch is up, the registry follows MultiKueueCluster events.
func TestWorkerConfigRegistry_FollowsEvents(t *testing.T) {
	clusters := &fakeClusters{}
	r := startRegistry(t, clusters)

	eventually(t, "the watch", func() bool { return clusters.currentWatcher() != nil })
	watcher := clusters.currentWatcher()

	clusters.serve("worker-1")
	watcher.Add(multiKueueCluster("worker-1"))
	eventually(t, "worker-1 to be loaded", hasClusters(r, "worker-1"))

	clusters.serve()
	watcher.Delete(multiKueueCluster("worker-1"))
	eventually(t, "worker-1 to be removed", hasClusters(r))
}
