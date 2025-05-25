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
	"slices"
	"sort"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	fastfuncv1 "fastgshare/fastfunc/api/v1"

	fastpodv1 "github.com/KontonGu/FaST-GShare/pkg/apis/fastgshare.caps.in.tum/v1"
	"github.com/prometheus/common/model"

	fasttypes "github.com/KontonGu/FaST-GShare/pkg/types"
)

type FastFunc struct {
	Name            string
	currentConfigs  []*Config
	fastPodToConfig map[string]*Config
	currentRPS      float64 //current rps of the function based on the current configs
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
			qps := fstfunc.Spec.MinQps

			klog.Infof("FaSTFunc %s has minQps = %d.", fstfunc.ObjectMeta.Name, qps)

			//func request
			funcRequest := &ResourceRequest{
				ModelName:      fstfunc.Spec.ModelName,
				QPS:            float64(qps), //just for testing
				AllocationType: fasttypes.AllocationType(fstfunc.Spec.AllocationType),
			}

			funcName := fstfunc.ObjectMeta.Name
			klog.Infof("Checking FaSTFunc %s.", funcName)

			isNew := fastFuncMap[fstfunc.ObjectMeta.Name] == nil

			// make a Prometheus query to get the RPS of the function
			query := fmt.Sprintf("rate(gateway_function_invocation_total{function_name='%s.%s'}[10s])", funcName, fstfunc.ObjectMeta.Namespace)
			klog.Infof("Prometheus Query: %s.", query)
			queryRes, _, err := r.promv1api.Query(ctx, query, time.Now())
			curRPS := float64(0.0)

			if err != nil {
				klog.Errorf("Error Failed to get RPS of function %s: %s.", funcName, err.Error())
				continue
			}

			if queryRes.(model.Vector).Len() != 0 {
				klog.Infof("Current rps vec for function %s is %v.", funcName, queryRes)
				curRPS = float64(queryRes.(model.Vector)[0].Value)
			}
			klog.Infof("Current rps for function %s is %f.", funcName, curRPS)

			curRPS = math.Max(curRPS, funcRequest.QPS)

			// make a Prometheus query to get the RPS of past 30s
			pastRPS := float64(0.0)
			pastquery := fmt.Sprintf("rate(gateway_function_invocation_total{function_name='%s.%s'}[30s])", funcName, fstfunc.ObjectMeta.Namespace)
			// klog.Infof("Prometheus Query: %s.", pastquery)
			pastqueryVec, _, err := r.promv1api.Query(ctx, pastquery, time.Now())
			if err != nil {
				klog.Errorf("Error Failed to get past 30s RPS of function %s.", funcName)
				continue
			}
			if pastqueryVec.(model.Vector).Len() != 0 {
				klog.Infof("Past 30s rps vec for function %s is %v.", funcName, pastqueryVec)
				pastRPS = float64(pastqueryVec.(model.Vector)[0].Value)
			}
			klog.Infof("Past 30s rps for function %s is %f.", funcName, pastRPS)

			// make a Prometheus query to get the RPS of old 30s
			oldRPS := float64(0.0)
			oldTime := time.Now().Add(-30 * time.Second)
			// klog.Infof("Prometheus Query: %s.", pastquery)
			oldqueryVec, _, err := r.promv1api.Query(ctx, pastquery, oldTime)
			if err != nil {
				klog.Errorf("Error Failed to get old 30s RPS of function %s.", funcName)
				continue
			}
			if oldqueryVec.(model.Vector).Len() != 0 {
				klog.Infof("Old 30s rps vec for function %s is %v.", funcName, oldqueryVec)
				oldRPS = float64(oldqueryVec.(model.Vector)[0].Value)
			}
			klog.Infof("Old 30s rps for function %s is %f.", funcName, oldRPS)

			err = r.UpdateFunction(&fstfunc, funcRequest, curRPS, pastRPS, oldRPS, isNew)
			if err != nil {
				klog.Errorf("Error Failed to update the state of the function %s.", funcName)
				continue
			}
		}
	}
}

// Get the desired FaSTFunc Specification for scaling
func (r *FaSTFuncReconciler) UpdateFunction(fastfunc *fastfuncv1.FaSTFunc,
	funcRequest *ResourceRequest,
	curRPS, pastRPS, oldRPS float64,
	isNew bool) error {
	funccc, ok := fastFuncMap[fastfunc.ObjectMeta.Name]
	var totalRPSCap float64
	if ok {
		totalRPSCap = funccc.currentRPS
	} else {
		totalRPSCap = curRPS
	}

	deltaReqs := curRPS - totalRPSCap

	scaleUp := deltaReqs > 0.2*totalRPSCap

	scaleDown := deltaReqs < 0 && -deltaReqs > 0.2*totalRPSCap

	if isNew {
		//new function

	}

	// rps difference
	klog.Infof("RPS difference (DeltaRPS) = %f.", deltaReqs)

	// KONTON Testing Start
	if scaleUp {
		funcRequest.QPS = deltaReqs
		klog.Infof("Scaling up the function %s. Requested QPS = %f, Current RPS = %f, Past RPS = %f, Old RPS = %f.", fastfunc.ObjectMeta.Name, funcRequest.QPS, curRPS, pastRPS, oldRPS)
		configslist, err := r.nodeManager.GetConfigs(funcRequest, true)
		configslist = r.nodeManager.PrepareConfigsRequirements(funcRequest, configslist)
		if err != nil {
			klog.Errorf("Error Failed to get configs for function %s.", fastfunc.ObjectMeta.Name)
			return nil
		}

		fastpods, _ := r.ConvertConfigs2FaSTPods(fastfunc, configslist)

		err = r.ReconcileFaSTPod(fastpods)
		if err != nil {
			klog.Errorf("Error Failed to reconcile the desired FaSTPods for function %s.", fastfunc.ObjectMeta.Name)
			return err
		}

	} else if scaleDown {
		// scaling down
		klog.Infof("Scaling down the function %s. Requested QPS = %f, Current RPS = %f, Past RPS = %f, Old RPS = %f.", fastfunc.ObjectMeta.Name, funcRequest.QPS, curRPS, pastRPS, oldRPS)
		deltaReqs = deltaReqs * (-1)
		funcRequest.QPS = deltaReqs
		r.scaleDownFaSTFunc(fastfunc.ObjectMeta.Name, curRPS)

	}
	return nil
}

// scaling down to update the replicas of fastpod of the FaSTFunc to be replica
func (r *FaSTFuncReconciler) scaleDownFaSTFunc(fastpodName string, newQPS float64) {

	//get current configs
	fastFunc, ok := fastFuncMap[fastpodName]
	if !ok {
		klog.Errorf("Error Failed to scaling down because of not finding the FaSTFunc %s.", fastpodName)
		return
	}

	currentConfigs := fastFunc.currentConfigs

	// Sort configs by AllocatedQPS descending (reduce larger QPS first)
	sort.Slice(currentConfigs, func(i, j int) bool {
		return currentConfigs[i].AllocatedQPS > currentConfigs[j].AllocatedQPS
	})

	currentQPS := 0.0
	for _, c := range currentConfigs {
		currentQPS += c.AllocatedQPS
	}

	toRemove := []*Config{}

	type ReduceInfo struct {
		Config     *Config
		NewReplica int
	}
	toReduce := make([]ReduceInfo, 0)

	for _, config := range currentConfigs {

		if currentQPS <= newQPS {
			break
		}

		qpsPerReplica := config.QpsPerReplica

		maxRemovableQPS := currentQPS - newQPS

		// How many replicas can we remove from this config?
		maxRemovableReplicas := int(math.Floor(maxRemovableQPS / qpsPerReplica))

		if maxRemovableReplicas >= config.AllocatedReplica {
			//remove the config if it can be removed completely
			toRemove = append(toRemove, config)
			currentQPS -= config.AllocatedQPS
			continue
		}

		//reduce the replicas of the config
		if maxRemovableReplicas > 0 {
			newReplica := config.AllocatedReplica - maxRemovableReplicas
			if newReplica < 1 {
				newReplica = 1 // Don't go below 1
			}
			reducedQPS := float64(newReplica) * qpsPerReplica
			updatedQPS := currentQPS - (config.AllocatedQPS - reducedQPS)
			//if the updated QPS is greater than the new QPS, then reduce the replicas

			if updatedQPS >= newQPS {
				currentQPS = updatedQPS
				toReduce = append(toReduce, ReduceInfo{Config: config, NewReplica: newReplica})
			}
		}
	}

	for _, config := range toRemove {
		//remove pod first

		fastPod := &fastpodv1.FaSTPod{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: fastpodName, Namespace: "fast-gshare-fn"}, fastPod)
		if err != nil {
			klog.Errorf("Error Failed to get the FaSTPod %s.", fastpodName)
			return
		}

		//delete the FaSTPod
		err = r.Delete(context.TODO(), fastPod)
		if err != nil {
			klog.Errorf("Error Failed to delete the FaSTPod %s.", fastpodName)
			return
		}

		//on deletion, update config remove all the configs that are related to the fastpod, release gpu if possible.

		//remove the config from the currentConfigs
		configIndex := slices.IndexFunc(currentConfigs, func(c *Config) bool {
			return c.Equal(config)
		})
		if configIndex != -1 {
			currentConfigs = slices.Delete(currentConfigs, configIndex, configIndex+1)
		}

	}
	for _, info := range toReduce {

		//get the FaSTPod
		fastPod := &fastpodv1.FaSTPod{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: fastpodName, Namespace: "fast-gshare-fn"}, fastPod)
		if err != nil {
			klog.Errorf("Error Failed to get the FaSTPod %s.", fastpodName)
			continue
		}
		//update the replicas of the FaSTPod
		existedCopy := fastPod.DeepCopy()
		replicaInt32 := int32(info.NewReplica)
		existedCopy.Spec.Replicas = &replicaInt32
		existedCopy.ObjectMeta.Labels["com.openfaas.scale.max"] = strconv.Itoa(info.NewReplica)
		existedCopy.ObjectMeta.Labels["com.openfaas.scale.min"] = strconv.Itoa(info.NewReplica)
		err = r.Update(context.TODO(), existedCopy)
		if err != nil {
			klog.Errorf("Error failed to update FaSTPod %s when scaling down.", fastpodName)

		}

		// reduce quota on existing config

	}

}
