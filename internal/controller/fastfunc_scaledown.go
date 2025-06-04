package controller

import (
	fastfuncv1 "fastgshare/fastfunc/api/v1"
	"math"

	fasttypes "github.com/KontonGu/FaST-GShare/pkg/types"
	"k8s.io/klog/v2"
)

// Get the desired FaSTFunc Specification for scaling
func (r *FaSTFuncReconciler) UpdateFunction(fastfunc *fastfuncv1.FaSTFunc,
	rps10s, rps30s, rps30s_earlier float64,
	minRps float64,
	preferredGPU string,
) error {
	klog.Infof("fastfunc: %s", fastfunc.ObjectMeta.Name)
	funccc, isOldFunction := fastFuncMap[fastfunc.ObjectMeta.Name]
	//if the function is not in the map, create a new one
	if funccc == nil {
		funccc = &FastFunc{
			Name:               fastfunc.ObjectMeta.Name,
			configUUIDToConfig: make(map[string]*Config),
		}
		fastFuncMap[fastfunc.ObjectMeta.Name] = funccc
	}
	var totalRPSCap float64
	if isOldFunction {
		totalRPSCap = funccc.currentRPSCapacity
	} else {
		totalRPSCap = 0
	}

	klog.Infof("isOldFunction: %t", isOldFunction)

	klog.Infof("totalRPSCap: %f", totalRPSCap)

	// rps10s = math.Max(rps10s, minRps)

	deltaReqs := math.Max(rps10s, minRps) - totalRPSCap

	scaleUp := deltaReqs >= 0.2*totalRPSCap || !isOldFunction

	scaleDown := deltaReqs < 0 && -deltaReqs > 0.2*totalRPSCap

	// rps difference
	klog.Infof("RPS difference (DeltaRPS) = %f.", deltaReqs)

	funcRequest := &ResourceRequest{
		ModelName:      fastfunc.Spec.ModelName,
		QPS:            float64(deltaReqs), //just for testing
		AllocationType: fasttypes.GetAllocationType(fastfunc.Spec.AllocationType),
		PreferredGPU:   preferredGPU,
	}

	// KONTON Testing Start
	if scaleUp {
		klog.Infof("Scaling up the function %s. Requested RPS = %f, Current RPS = %f, Past RPS = %f, Old RPS = %f. minRps = %f",
			fastfunc.ObjectMeta.Name, funcRequest.QPS, rps10s, rps30s, rps30s_earlier, minRps)
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
				r.scheduleForDeletion(config.Config.associatedGpu)
			}
			funccc.configUUIDToConfig[config.Config.UUID] = config.Config
		}
		for _, config := range configsRemoved {
			funccc.currentRPSCapacity -= config.AllocatedRPS
			//remove config from gpu first
			isEmpty := config.associatedGpu.DeallocateConfig(config)
			if isEmpty {
				r.scheduleForDeletion(config.associatedGpu)
			}
			delete(funccc.configUUIDToConfig, config.UUID)
		}

		//log

	}

	return nil
}
