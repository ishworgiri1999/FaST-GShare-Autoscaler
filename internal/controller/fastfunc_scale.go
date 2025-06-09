package controller

import (
	fastfuncv1 "fastgshare/fastfunc/api/v1"
	"fmt"
	"log"
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
		totalRPSCap = funccc.currentRPSCapacity * 0.8 //assume 20% reduced capacity
	} else {
		totalRPSCap = 0
	}

	decreasingTrend := rps10s-rps30s <= 0

	recentGrowth := rps10s - rps30s_earlier
	hasGrown := rps10s - rps30s_earlier

	predictedRate := rps10s
	if recentGrowth > 0 {
		predictedRate = rps10s + recentGrowth
	}
	// Cap predictedRate to be at least minRps
	predictedRate = math.Max(predictedRate, minRps)

	// scale up
	estimatedFutureDemand := predictedRate * 1.1 // Always be ready for 1.1x queries
	deltaUp := estimatedFutureDemand - totalRPSCap

	var loadRatio float64
	if totalRPSCap > 0 {
		loadRatio = rps10s / totalRPSCap
	} else {
		loadRatio = 0
	}

	scaleDownThreshold := 0.5

	fmt.Printf("10s Rate: %.2f req/s\n", rps10s)
	fmt.Printf("Predicted Rate: %.2f req/s\n", predictedRate)
	fmt.Printf("Capacity: %.2f req/s\n", totalRPSCap)
	fmt.Printf("Load Ratio: %.2f\n", loadRatio)
	fmt.Printf("Delta Up: %.2f req/s\n", deltaUp)
	fmt.Printf("Scale Down Threshold: %.2f\n", scaleDownThreshold)

	fmt.Printf("Recent Growth: %.2f req/s\n", recentGrowth)
	fmt.Printf("Has Grown: %.2f req/s\n", hasGrown)
	fmt.Printf("Decreasing Trend: %t\n", decreasingTrend)

	shouldScaleUp := false
	shouldScaleDown := false

	scaleDownConsecutiveRequired := 3 // Number of consecutive checks required

	if deltaUp > 0 || !isOldFunction {
		shouldScaleUp = true
		funccc.scaleDownConsecutiveCount = 0 // Reset scale-down counter on scale up
		fmt.Printf("⚠️  Estimated upcoming delta: %.2f req/s — scale up\n", deltaUp)
	} else if loadRatio < scaleDownThreshold && len(funccc.CurrentConfigs()) > 1 && (decreasingTrend) {
		funccc.scaleDownConsecutiveCount++
		fmt.Printf("Scale down condition met %d/%d times\n", funccc.scaleDownConsecutiveCount, scaleDownConsecutiveRequired)
		if funccc.scaleDownConsecutiveCount >= scaleDownConsecutiveRequired {
			shouldScaleDown = true
			funccc.scaleDownConsecutiveCount = 0 // Reset after triggering scale down
			fmt.Printf("⚠️  Estimated upcoming delta: %.2f req/s — scale down\n", deltaUp)
		}
	} else {
		fmt.Println("✅ Load is stable — no immediate scaling needed.")
		shouldScaleUp = false
		funccc.scaleDownConsecutiveCount = 0 // Reset if not met
	}

	funcRequest := &ResourceRequest{
		ModelName:      fastfunc.Spec.ModelName,
		QPS:            float64(deltaUp), //just for testing
		AllocationType: fasttypes.GetAllocationType(fastfunc.Spec.AllocationType),
		PreferredGPU:   preferredGPU,
	}

	// KONTON Testing Start
	if shouldScaleUp {
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

		//if congis are more than 1, then we can scale down
	} else if shouldScaleDown && len(funccc.CurrentConfigs()) > 1 {

		// scaling down
		klog.Infof("Scaling down the function %s. Requested RPS = %f, Predicted RPS = %f, Past RPS = %f, Old RPS = %f.",
			fastfunc.ObjectMeta.Name, funcRequest.QPS, predictedRate, rps30s, rps30s_earlier)
		// deltaReqs = deltaReqs * (-1)
		configsUpdated, configsRemoved := r.scaleDownFaSTFunc(funccc, predictedRate)
		for _, config := range configsUpdated {
			log.Printf("Removing %d replicas from config %s", config.NewReplica, config.Config.UUID)
			funccc.currentRPSCapacity -= config.ReducedDelta
			//update config in gpu
			isEmpty := config.Config.associatedGpu.ReduceConfig(config.Config, config.NewReplica)
			if isEmpty {
				r.scheduleForDeletion(config.Config.associatedGpu)
			}
			funccc.configUUIDToConfig[config.Config.UUID] = config.Config
		}
		for _, config := range configsRemoved {
			log.Printf("Deleting config %s with %d replicas", config.UUID, config.AllocatedReplica)
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
