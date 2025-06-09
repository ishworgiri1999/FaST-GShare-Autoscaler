/*
Copyright 2024 FaST-GShare Authors, KontonGu (Jianfeng Gu), et. al.
@Techinical University of Munich, CAPS Cloud Team

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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	fastfuncv1 "fastgshare/fastfunc/api/v1"
	"fastgshare/fastfunc/internal/profiling"

	fastpodv1 "github.com/KontonGu/FaST-GShare/pkg/apis/fastgshare.caps.in.tum/v1"
	fastpodclientset "github.com/KontonGu/FaST-GShare/pkg/client/clientset/versioned"
	fastpodinformer "github.com/KontonGu/FaST-GShare/pkg/client/informers/externalversions"
	fastpodlisters "github.com/KontonGu/FaST-GShare/pkg/client/listers/fastgshare.caps.in.tum/v1"
	"github.com/KontonGu/FaST-GShare/pkg/proto/seti/v1"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	kubeinformers "k8s.io/client-go/informers"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type InitConfig struct {
	PrometheusURL       string
	NodeListenerAddress string
}

// FaSTFuncReconciler reconciles a FaSTFunc object
type FaSTFuncReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	promv1api        promv1.API
	fastpodLister    fastpodlisters.FaSTPodLister
	nodesLister      corelisters.NodeLister
	nodeManager      *NodeManager
	gpuReleaseQueue  workqueue.TypedRateLimitingInterface[GPUReleaseWorkItem]
	Log              *logr.Logger
	gpuWorkerStarted bool
	processingGPUs   map[string]bool
}

type GPUReleaseWorkItem struct {
	GPUUUID     string
	FastFuncRef types.NamespacedName
	NodeName    string
	RetryCount  int
	Timestamp   time.Time
}

// Key generates a unique key for the work item
func (item GPUReleaseWorkItem) Key() string {
	return fmt.Sprintf("%s/%s", item.FastFuncRef.String(), item.GPUUUID)
}

type FaSTPodConfig struct {
	Quota       int // 1-100 (fraction of seconds. total is 100)
	SMPartition int // percentage (0-100)
	Mem         int64
	Replicas    int64
	GPUType     string
	GPUUUID     string
	NodeName    string
	IsMig       bool
}

var once sync.Once

// +kubebuilder:rbac:groups=caps.in.tum.fastgshare,resources=fastfuncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=caps.in.tum.fastgshare,resources=fastfuncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=caps.in.tum.fastgshare,resources=fastfuncs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *FaSTFuncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Try to get the FaSTFunc
	var fastFunc fastfuncv1.FaSTFunc
	err := r.Get(ctx, req.NamespacedName, &fastFunc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// FaSTFunc was deleted, perform cleanup
			log.Info("FaSTFunc deleted, performing cleanup", "name", req.NamespacedName)
			r.handleDeletedFaSTFunc(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		// Other error
		return ctrl.Result{}, err
	}

	// Initialize workqueue if not done
	if r.gpuReleaseQueue == nil {
		r.gpuReleaseQueue = workqueue.NewTypedRateLimitingQueue(
			workqueue.DefaultTypedControllerRateLimiter[GPUReleaseWorkItem](),
		)
		r.processingGPUs = make(map[string]bool)
	}

	// Start GPU release worker if not started
	if !r.gpuWorkerStarted {
		go r.runGPUReleaseWorker()
		r.gpuWorkerStarted = true
	}

	once.Do(func() {
		go r.persistentReconcile(ctx)
	})

	return ctrl.Result{}, nil
}

// runGPUReleaseWorker processes GPU release tasks sequentially
func (r *FaSTFuncReconciler) runGPUReleaseWorker() {

	log := r.Log.WithName("gpu-release-worker")
	log.Info("Starting GPU release worker")

	for func() bool {
		// Process next item from queue
		item, shutdown := r.gpuReleaseQueue.Get()

		if shutdown {
			log.Info("GPU release queue shut down")
			return false
		}

		// Process the item
		err, tryAgain := r.processGPUReleaseItem(item)
		if err != nil {
			log.Error(err, "Error processing GPU release item")
			if tryAgain {
				if item.RetryCount > 10 {
					log.Error(fmt.Errorf("failed to release GPU after 3 retries"), "Failed to release GPU", "gpu", item.GPUUUID, "node", item.NodeName)
					r.gpuReleaseQueue.Done(item)
					return true
				}
				//increment failure count
				retryCount := item.RetryCount + 1

				//check if the item is already in the queue
				if _, exists := r.processingGPUs[item.Key()]; !exists {
					r.gpuReleaseQueue.Add(GPUReleaseWorkItem{
						GPUUUID:    item.GPUUUID,
						NodeName:   item.NodeName,
						RetryCount: retryCount,
						Timestamp:  time.Now(),
					})
				}
			}
		}

		// Mark item as done
		r.gpuReleaseQueue.Done(item)
		return true
	}() {
		//do nothing
	}

}

func (r *FaSTFuncReconciler) processGPUReleaseItem(item interface{}) (error, bool) {
	log := r.Log.WithName("gpu-release-processor")

	workItem, ok := item.(GPUReleaseWorkItem)
	if !ok {
		log.Error(fmt.Errorf("invalid work item type"), "Expected GPUReleaseWorkItem")
		return fmt.Errorf("invalid work item type"), false
	}

	key := workItem.Key()

	// Mark as processing
	r.processingGPUs[key] = true
	defer func() {
		delete(r.processingGPUs, key)
	}()

	log.Info("Processing GPU release",
		"gpu", workItem.GPUUUID,
		"fastfunc", workItem.FastFuncRef,
		"retries", workItem.RetryCount)

	// lock node
	node, ok := r.nodeManager.nodes[workItem.NodeName]
	if !ok {
		log.Error(fmt.Errorf("node not found"), "Node not found", "node", workItem.NodeName)
		return fmt.Errorf("node not found"), false
	}
	node.lock.Lock()
	defer node.lock.Unlock()

	if node != nil {
		resp, err := node.GrpcClient.ReleaseVirtualGPU(context.TODO(), &seti.ReleaseVirtualGPURequest{
			Uuid: workItem.GPUUUID,
		})

		if err != nil {
			return fmt.Errorf("error releasing virtual GPU"), true
		}

		node.availableGPUs = append(node.availableGPUs, resp.AvailableVirtualGpus...)

		delete(node.physicalGPUsMap, workItem.GPUUUID)

	}

	return nil, false
}

// Initialize the FaSTPod Lister
func getFaSTPodLister(client fastpodclientset.Interface, namespace string, stopCh chan struct{}) fastpodlisters.FaSTPodLister {
	// create a shared informer factory for the FaasShare API group
	informerFactory := fastpodinformer.NewSharedInformerFactoryWithOptions(
		client,
		0,
		fastpodinformer.WithNamespace(namespace),
	)
	// retrieve the shared informer for FaSTPods
	fastpodInformer := informerFactory.Fastgshare().V1().FaSTPods().Informer()
	informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, fastpodInformer.HasSynced) {
		return nil
	}
	// create a lister for FaSTPods using the shared informer's indexers
	fastpodLister := fastpodlisters.NewFaSTPodLister(fastpodInformer.GetIndexer())
	return fastpodLister
}

func getNodeLister(client kubernetes.Interface, stopCh chan struct{}) corelisters.NodeLister {
	// create a shared informer factory for the FaasShare API group
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(client, time.Second*30)

	nodeInformer := kubeInformerFactory.Core().V1().Nodes().Informer()
	kubeInformerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(stopCh, nodeInformer.HasSynced) {
		return nil
	}
	nodeLister := corelisters.NewNodeLister(nodeInformer.GetIndexer())
	return nodeLister
}

// SetupWithManager sets up the controller with the Manager.
func (r *FaSTFuncReconciler) SetupWithManager(mgr ctrl.Manager, initConfig InitConfig) error {

	// Create a Prometheus API client
	promClient, err := api.NewClient(api.Config{
		Address: initConfig.PrometheusURL,
	})
	if err != nil {
		ctrl.Log.Error(err, "Failed to create the Prometheus client.")
		return err
	}

	r.promv1api = promv1.NewAPI(promClient)

	client, _ := fastpodclientset.NewForConfig(ctrl.GetConfigOrDie())
	kubeClient, _ := kubernetes.NewForConfig(ctrl.GetConfigOrDie())

	stopCh := make(chan struct{})
	r.nodeManager = NewNodeManager(5, profiling.QpsStore)
	go r.nodeManager.StartTCPAcceptor(initConfig.NodeListenerAddress, stopCh)

	ctrl.Log.Info("Starting the FaSTFunc controller")
	r.fastpodLister = getFaSTPodLister(client, "fast-gshare-fn", stopCh)
	r.nodesLister = getNodeLister(kubeClient, stopCh)
	fastpodv1.AddToScheme(r.Scheme)

	return ctrl.NewControllerManagedBy(mgr).
		For(&fastfuncv1.FaSTFunc{}).
		Complete(r)
}

func (r *FaSTFuncReconciler) handleDeletedFaSTFunc(name types.NamespacedName) {
	//removing the fastfunc from the fastfunc map
	//get fastfunc name from fastfunc map
	fastfunc, ok := fastFuncMap[name.Name]
	if !ok {
		return
	}

	//release all GPUs
	for _, config := range fastfunc.CurrentConfigs() {
		isEmpty := config.associatedGpu.ReduceConfig(config, 0)
		if isEmpty && config.associatedGpu.virtual {
			//release the GPU
			r.scheduleForDeletion(config.associatedGpu)
		}
	}

	//remove from fastfunc map
	delete(fastFuncMap, name.Name)
}

// --- API Server for current configs ---
var apiServerOnce sync.Once

func startAPIServer() {
	http.HandleFunc("/current-configs", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "Missing 'name' query parameter", http.StatusBadRequest)
			return
		}

		fastfunc, ok := fastFuncMap[name]
		if !ok {
			http.Error(w, "Function not found", http.StatusNotFound)
			return
		}

		configs := fastfunc.CurrentConfigs()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(configs); err != nil {
			http.Error(w, "Failed to encode configs", http.StatusInternalServerError)
		}
	})
	go func() {
		if err := http.ListenAndServe(":9001", nil); err != nil {
			// Optionally log error
		}
	}()
}

func init() {
	apiServerOnce.Do(startAPIServer)
}
