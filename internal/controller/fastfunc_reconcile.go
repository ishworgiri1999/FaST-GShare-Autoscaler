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
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	fastfuncv1 "fastgshare/fastfunc/api/v1"

	fastpodv1 "github.com/KontonGu/FaST-GShare/pkg/apis/fastgshare.caps.in.tum/v1"
	"github.com/KontonGu/FaST-GShare/pkg/proto/seti/v1"
	fasttypes "github.com/KontonGu/FaST-GShare/pkg/types"
	"github.com/prometheus/common/model"
)

type FastFunc struct {
	Name               string
	configUUIDToConfig map[string]*Config
	currentRPSCapacity float64 //current rps of the function based on the current configs
}

var failedReleasesGPUs = make(map[string]*GPUInfo)

func (f *FastFunc) CurrentConfigs() []*Config {
	configs := make([]*Config, 0)
	for _, config := range f.configUUIDToConfig {
		configs = append(configs, config)
	}
	return configs
}

var fastFuncMap = make(map[string]*FastFunc)

func (r *FaSTFuncReconciler) persistentReconcile(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {

		nodeList, err := r.nodesLister.List(labels.Set{"gpu": "present"}.AsSelector())
		if err != nil {
			klog.Errorf("error Cannot find gpu node with the lable \"gpu:present\"")
			continue
		}
		for _, node := range nodeList {
			klog.Infof("Node: %s", node.Name)
		}

		var allFaSTfuncs fastfuncv1.FaSTFuncList
		if err := r.List(ctx, &allFaSTfuncs); err != nil {
			klog.Error(err, "Failed to get FaSTFuncs.")
			continue
		}

		// reconcile for each FaSTFunc
		for _, fstfunc := range allFaSTfuncs.Items {
			rps := fstfunc.Spec.MinQps

			klog.Infof("FaSTFunc %s has minQps = %d.", fstfunc.ObjectMeta.Name, rps)

			funcName := fstfunc.ObjectMeta.Name
			klog.Infof("Checking FaSTFunc %s.", funcName)

			// make a Prometheus query to get the RPS of the function
			query := fmt.Sprintf("rate(gateway_function_invocation_total{function_name='%s.%s'}[10s])", funcName, fstfunc.ObjectMeta.Namespace)
			klog.Infof("Prometheus Query: %s.", query)
			queryRes, _, err := r.promv1api.Query(ctx, query, time.Now())
			rps10s := float64(0.0)

			if err != nil {
				klog.Errorf("Error Failed to get RPS of function %s: %s.", funcName, err.Error())
				continue
			}

			if queryRes.(model.Vector).Len() != 0 {
				klog.Infof("Current rps vec for function %s is %v.", funcName, queryRes)
				rps10s = float64(queryRes.(model.Vector)[0].Value)
			}
			klog.Infof("Current rps for function %s is %f.", funcName, rps10s)

			// make a Prometheus query to get the RPS of past 30s
			rps30s := float64(0.0)
			pastquery := fmt.Sprintf("rate(gateway_function_invocation_total{function_name='%s.%s'}[30s])", funcName, fstfunc.ObjectMeta.Namespace)
			// klog.Infof("Prometheus Query: %s.", pastquery)
			pastqueryVec, _, err := r.promv1api.Query(ctx, pastquery, time.Now())
			if err != nil {
				klog.Errorf("Error Failed to get past 30s RPS of function %s.", funcName)
				continue
			}
			if pastqueryVec.(model.Vector).Len() != 0 {
				klog.Infof("Past 30s rps vec for function %s is %v.", funcName, pastqueryVec)
				rps30s = float64(pastqueryVec.(model.Vector)[0].Value)
			}
			klog.Infof("Past 30s rps for function %s is %f.", funcName, rps30s)

			// make a Prometheus query to get the RPS of old 30s in past time.
			rps30s_earlier := float64(0.0) //before 30s
			// klog.Infof("Prometheus Query: %s.", pastquery)
			oldqueryVec, _, err := r.promv1api.Query(ctx, pastquery, time.Now().Add(-30*time.Second))
			if err != nil {
				klog.Errorf("Error Failed to get old 30s RPS of function %s.", funcName)
				continue
			}
			if oldqueryVec.(model.Vector).Len() != 0 {
				klog.Infof("Old 30s rps vec for function %s is %v.", funcName, oldqueryVec)
				rps30s_earlier = float64(oldqueryVec.(model.Vector)[0].Value)
			}
			klog.Infof("Old 30s rps for function %s is %f.", funcName, rps30s_earlier)

			err = r.UpdateFunction(&fstfunc, rps10s, rps30s, rps30s_earlier, float64(rps))
			if err != nil {
				klog.Errorf("Error Failed to update the state of the function %s.", funcName)
				continue
			}

		}
	}
}

// Get the desired FaSTFunc Specification for scaling
func (r *FaSTFuncReconciler) UpdateFunction(fastfunc *fastfuncv1.FaSTFunc,
	rps10s, rps30s, rps30s_earlier float64,
	minRps float64,
) error {
	funccc, isOldFunction := fastFuncMap[fastfunc.ObjectMeta.Name]
	//if the function is not in the map, create a new one
	if funccc == nil {
		funccc = &FastFunc{
			Name:               fastfunc.ObjectMeta.Name,
			configUUIDToConfig: make(map[string]*Config),
		}
	}
	var totalRPSCap float64
	if isOldFunction {
		totalRPSCap = funccc.currentRPSCapacity
	} else {
		totalRPSCap = 0
	}

	deltaReqs := rps10s - totalRPSCap

	scaleUp := deltaReqs >= 0.2*totalRPSCap || !isOldFunction

	scaleDown := deltaReqs < 0 && -deltaReqs > 0.2*totalRPSCap

	// rps difference
	klog.Infof("RPS difference (DeltaRPS) = %f.", deltaReqs)

	funcRequest := &ResourceRequest{
		ModelName:      fastfunc.Spec.ModelName,
		QPS:            float64(deltaReqs), //just for testing
		AllocationType: fasttypes.GetAllocationType(fastfunc.Spec.AllocationType),
	}

	// KONTON Testing Start
	if scaleUp {
		klog.Infof("Scaling up the function %s. Requested RPS = %f, Current RPS = %f, Past RPS = %f, Old RPS = %f.",
			fastfunc.ObjectMeta.Name, funcRequest.QPS, rps10s, rps30s, rps30s_earlier)
		configslist, err := r.nodeManager.GetConfigs(funcRequest, !isOldFunction)
		newConfigList := r.nodeManager.PrepareConfigsRequirements(funcRequest, configslist)
		if err != nil {
			// TODO: handle this error
			klog.Errorf("Error: %v", err)
			klog.Errorf("Error Failed to get configs for function %s.", fastfunc.ObjectMeta.Name)
			return nil
		}

		fastpods, _ := r.ConvertConfigs2FaSTPods(fastfunc, newConfigList)

		err = r.ReconcileFaSTPod(fastpods)
		if err != nil {
			klog.Errorf("Error Failed to reconcile the desired FaSTPods for function %s.", fastfunc.ObjectMeta.Name)
			return err
		}
		//add configs
		for _, config := range newConfigList {
			funccc.configUUIDToConfig[config.UUID] = config
			funccc.currentRPSCapacity += config.AllocatedRPS
		}

	} else if scaleDown {
		// scaling down
		klog.Infof("Scaling down the function %s. Requested RPS = %f, Current RPS = %f, Past RPS = %f, Old RPS = %f.",
			fastfunc.ObjectMeta.Name, funcRequest.QPS, rps10s, rps30s, rps30s_earlier)
		// deltaReqs = deltaReqs * (-1)
		configsUpdated, configsRemoved := r.scaleDownFaSTFunc(funccc, rps10s)
		for _, config := range configsUpdated {
			funccc.currentRPSCapacity -= config.ReducedDelta
			//update config in gpu
			isEmpty := config.Config.associatedGpu.ReduceConfig(config.Config, config.NewReplica)
			if isEmpty {
				r.destroyGPU(config.Config.associatedGpu)
			}
			funccc.configUUIDToConfig[config.Config.UUID] = config.Config
		}
		for _, config := range configsRemoved {
			funccc.currentRPSCapacity -= config.AllocatedRPS
			//remove config from gpu first
			isEmpty := config.associatedGpu.DeallocateConfig(config)
			if isEmpty {
				r.destroyGPU(config.associatedGpu)
			}
			delete(funccc.configUUIDToConfig, config.UUID)
		}
	}

	return nil
}

func (r *FaSTFuncReconciler) destroyGPU(gpu *GPUInfo) {

	if gpu.Usage.IsEmpty() && gpu.virtual {
		node := r.nodeManager.nodes[gpu.NodeName]
		node.lock.Lock()
		defer node.lock.Unlock()
		if node != nil {
			resp, err := node.GrpcClient.ReleaseVirtualGPU(context.TODO(), &seti.ReleaseVirtualGPURequest{
				Uuid: gpu.UUID,
			})

			if err != nil {
				klog.Errorf("Error Failed to release the virtual GPU %s.", gpu.UUID)
				return
			}

			node.availableGPUs = append(node.availableGPUs, resp.AvailableVirtualGpus...)

			delete(node.physicalGPUsMap, gpu.UUID)

		}
	}

}

type ReduceInfo struct {
	Config       *Config
	NewReplica   int
	ReducedDelta float64
}

// scaling down to update the replicas of fastpod of the FaSTFunc to be replica
func (r *FaSTFuncReconciler) scaleDownFaSTFunc(fastFunc *FastFunc, newRPS float64) ([]*ReduceInfo, []*Config) {

	currentConfigs := fastFunc.CurrentConfigs()

	// Sort configs by AllocatedRPS descending (reduce larger RPS first)
	sort.Slice(currentConfigs, func(i, j int) bool {
		return currentConfigs[i].AllocatedRPS > currentConfigs[j].AllocatedRPS
	})

	currentRPS := 0.0
	for _, c := range currentConfigs {
		currentRPS += c.AllocatedRPS
	}

	toRemove := []*Config{}

	toUpdate := make([]*ReduceInfo, 0)

	var configsUpdated []*ReduceInfo
	var configsRemoved []*Config

	for _, config := range currentConfigs {

		if currentRPS <= newRPS {
			break
		}

		rpsPerReplica := config.QpsPerReplica

		maxRemovableRPS := currentRPS - newRPS
		// How many replicas can we remove from this config?
		maxRemovableReplicas := int(math.Floor(maxRemovableRPS / rpsPerReplica))
		if maxRemovableReplicas >= config.AllocatedReplica {
			//if current after removal is still greater than newRPS, then we can remove the config
			if currentRPS-config.AllocatedRPS > newRPS {
				currentRPS -= config.AllocatedRPS
				toRemove = append(toRemove, config)

			} else { //if current after removal is less than newRPS, then we can't remove the config and should break
				break
			}

		}

		//reduce the replicas of the config
		if maxRemovableReplicas > 0 {
			newReplica := config.AllocatedReplica - maxRemovableReplicas
			if newReplica < 1 {
				newReplica = 1 // Don't go below 1
			}
			updatedRPS := float64(newReplica) * rpsPerReplica
			reducedDelta := currentRPS - (config.AllocatedRPS - updatedRPS)
			//if the updated RPS is greater than the new RPS, then reduce the replicas

			//if current after reduction is still greater than newRPS, then we can reduce the replicas
			if currentRPS-reducedDelta > newRPS {
				currentRPS -= reducedDelta
				toUpdate = append(toUpdate, &ReduceInfo{Config: config, NewReplica: newReplica, ReducedDelta: reducedDelta})
			} else { //if current after reduction is less than newRPS, then we can't reduce the replicas and should break
				break
			}
		}
	}

	for _, config := range toRemove {
		//remove pod first

		fastPod := &fastpodv1.FaSTPod{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: getFastPodName(fastFunc.Name, config.UUID), Namespace: "fast-gshare-fn"}, fastPod)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Infof("FaSTPod %s not found, skipping.", getFastPodName(fastFunc.Name, config.UUID))
				configsRemoved = append(configsRemoved, config)
				continue
			}

			klog.Errorf("Error Failed to get the FaSTPod %s.", getFastPodName(fastFunc.Name, config.UUID))
			continue
		}

		//delete the FaSTPod
		err = r.Delete(context.TODO(), fastPod)
		if err != nil {
			klog.Errorf("Error Failed to delete the FaSTPod %s.", getFastPodName(fastFunc.Name, config.UUID))
			continue
		}

		configsRemoved = append(configsRemoved, config)

	}

	for _, updateInfo := range toUpdate {

		//get the FaSTPod
		fastPod := &fastpodv1.FaSTPod{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: getFastPodName(fastFunc.Name, updateInfo.Config.UUID), Namespace: "fast-gshare-fn"}, fastPod)
		if err != nil {
			if errors.IsNotFound(err) {
				klog.Infof("FaSTPod %s not found, skipping.", getFastPodName(fastFunc.Name, updateInfo.Config.UUID))
				configsUpdated = append(configsUpdated, updateInfo)
				continue
			}
			klog.Errorf("Error Failed to get the FaSTPod %s.", getFastPodName(fastFunc.Name, updateInfo.Config.UUID))
			continue
		}
		//update the replicas of the FaSTPod
		existedCopy := fastPod.DeepCopy()
		replicaInt32 := int32(updateInfo.NewReplica)
		existedCopy.Spec.Replicas = &replicaInt32
		existedCopy.ObjectMeta.Labels["com.openfaas.scale.max"] = strconv.Itoa(updateInfo.NewReplica)
		existedCopy.ObjectMeta.Labels["com.openfaas.scale.min"] = strconv.Itoa(updateInfo.NewReplica)
		err = r.Update(context.TODO(), existedCopy)
		if err != nil {
			klog.Errorf("Error failed to update FaSTPod %s when scaling down.", getFastPodName(fastFunc.Name, updateInfo.Config.UUID))
			continue
		}
		configsUpdated = append(configsUpdated, updateInfo)

	}

	return configsUpdated, configsRemoved

}
