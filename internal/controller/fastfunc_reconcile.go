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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	fastfuncv1 "fastgshare/fastfunc/api/v1"

	fastpodv1 "github.com/KontonGu/FaST-GShare/pkg/apis/fastgshare.caps.in.tum/v1"
	"github.com/prometheus/common/model"
)

type FastFunc struct {
	Name               string
	configUUIDToConfig map[string]*Config
	currentRPSCapacity float64 //current rps of the function based on the current configs
	// Number of consecutive times scale-down condition has been met
	scaleDownConsecutiveCount int
}

func (f *FastFunc) GetConfigForFastPod(fastPod *fastpodv1.FaSTPod) *Config {
	configUUID := fastPod.ObjectMeta.Annotations["config_uuid"]
	config, ok := f.configUUIDToConfig[configUUID]
	if !ok {
		return nil
	}
	return config
}

func (f *FastFunc) CurrentConfigs() []*Config {
	configs := make([]*Config, 0)
	for _, config := range f.configUUIDToConfig {
		configs = append(configs, config)
	}
	return configs
}

var fastFuncMap = make(map[string]*FastFunc)

func (r *FaSTFuncReconciler) doPromQuery(ctx context.Context, query string, ts time.Time) (float64, error) {
	queryRes, _, err := r.promv1api.Query(ctx, query, ts)
	if err != nil {
		return 0, err
	}
	if queryRes.(model.Vector).Len() != 0 {
		//sum up the values
		sum := 0.0
		for _, v := range queryRes.(model.Vector) {
			klog.Infof("v.Value: %f", v.Value)
			sum += float64(v.Value)
		}
		return sum, nil
	}
	return 0, nil
}

func (r *FaSTFuncReconciler) getRPSMetrics(ctx context.Context, funcName, namespace string) (rps10s, rps30s, rps30sEarlier float64, err error) {
	query10s := fmt.Sprintf("rate(gateway_function_invocation_total{function_name='%s.%s'}[10s])", funcName, namespace)
	query30s := fmt.Sprintf("rate(gateway_function_invocation_total{function_name='%s.%s'}[30s])", funcName, namespace)

	now := time.Now()
	rps10s, err = r.doPromQuery(ctx, query10s, now)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to get 10s RPS: %w", err)
	}

	rps30s, err = r.doPromQuery(ctx, query30s, now)
	if err != nil {
		return rps10s, 0, 0, fmt.Errorf("failed to get 30s RPS: %w", err)
	}

	rps30sEarlier, err = r.doPromQuery(ctx, query30s, now.Add(-30*time.Second))
	if err != nil {
		return rps10s, rps30s, 0, fmt.Errorf("failed to get old 30s RPS: %w", err)
	}

	return rps10s, rps30s, rps30sEarlier, nil
}

func (r *FaSTFuncReconciler) persistentReconcile(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)

	defer ticker.Stop()
	for range ticker.C {

		var fastFuncList fastfuncv1.FaSTFuncList
		if err := r.List(ctx, &fastFuncList); err != nil {
			klog.Error(err, "Failed to get FaSTFuncs.")
			continue
		}

		//log if empty
		if len(fastFuncList.Items) == 0 {
			klog.Infof("No FaSTFuncs found. Reconcile done.")
			continue
		}

		// reconcile for each FaSTFunc
		for _, fstfunc := range fastFuncList.Items {
			rps := fstfunc.Spec.MinQps

			preferredGPU := ""
			if fstfunc.ObjectMeta.Annotations != nil && fstfunc.ObjectMeta.Annotations["gpu-preferred"] != "" {
				preferredGPU = fstfunc.ObjectMeta.Annotations["gpu-preferred"]
			}

			if fstfunc.ObjectMeta.Annotations != nil && fstfunc.ObjectMeta.Annotations["qps-to-maintain"] != "" {
				var err error
				rps, err = strconv.Atoi(fstfunc.ObjectMeta.Annotations["qps-to-maintain"])
				if err != nil {
					klog.Errorf("Error Failed to get qps-to-maintain for function %s.", fstfunc.ObjectMeta.Name)
					continue
				}
				rps = int(rps)
			}

			klog.Infof("FaSTFunc %s has qps to maintain = %d, minQps = %d.", fstfunc.ObjectMeta.Name, rps, fstfunc.Spec.MinQps)

			funcName := fstfunc.Name
			klog.Infof("Checking FaSTFunc %s.", funcName)

			// make a Prometheus query to get the RPS of the function
			rps10s, rps30s, rps30s_earlier, err := r.getRPSMetrics(ctx, funcName, fstfunc.ObjectMeta.Namespace)
			if err != nil {
				klog.Errorf("Error Failed to get RPS metrics of function %s: %s.", funcName, err.Error())
				continue
			}
			klog.Infof("Current rps for function %s is %f.", funcName, rps10s)
			klog.Infof("Past 30s rps for function %s is %f.", funcName, rps30s)
			klog.Infof("Old 30s rps for function %s is %f.", funcName, rps30s_earlier)

			err = r.UpdateFunction(&fstfunc, rps10s, rps30s, rps30s_earlier, float64(rps), preferredGPU)
			if err != nil {
				klog.Errorf("Error Failed to update the state of the function %s.", funcName)
				continue
			}

		}
	}
}

func (r *FaSTFuncReconciler) scheduleForDeletion(gpu *GPUInfo) {

	time.AfterFunc(10*time.Second, func() {
		r.gpuReleaseQueue.Add(GPUReleaseWorkItem{
			GPUUUID:    gpu.UUID,
			NodeName:   gpu.NodeName,
			RetryCount: 0,
			Timestamp:  time.Now(),
		})
	})

}

type ReduceInfo struct {
	Config       *Config
	NewReplica   int
	ReducedDelta float64
}

// scaling down to update the replicas of fastpod of the FaSTFunc to be replica
func (r *FaSTFuncReconciler) scaleDownFaSTFunc(fastFunc *FastFunc, newRPS float64) ([]*ReduceInfo, []*Config) {

	currentConfigs := fastFunc.CurrentConfigs()

	// Sort configs by AllocatedRPS ascending (reduce smaller RPS first)
	sort.Slice(currentConfigs, func(i, j int) bool {
		return currentConfigs[i].AllocatedRPS < currentConfigs[j].AllocatedRPS
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
			if currentRPS-config.AllocatedRPS >= newRPS {
				currentRPS -= config.AllocatedRPS
				toRemove = append(toRemove, config)

				continue

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
			if currentRPS-reducedDelta >= newRPS {
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
			klog.Errorf("Error: %s", err.Error())
			klog.Errorf("Error failed to update FaSTPod %s when scaling down.", getFastPodName(fastFunc.Name, updateInfo.Config.UUID))
			continue
		}
		configsUpdated = append(configsUpdated, updateInfo)

	}

	return configsUpdated, configsRemoved

}
