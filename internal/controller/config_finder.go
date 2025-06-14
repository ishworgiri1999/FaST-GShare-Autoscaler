package controller

import (
	"context"
	"fastgshare/fastfunc/internal/shelf"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/KontonGu/FaST-GShare/pkg/proto/seti/v1"
	"github.com/KontonGu/FaST-GShare/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/klog/v2"
)

type ResourceRequest struct {
	ModelName      string
	QPS            float64
	AllocationType types.AllocationType
	PreferredGPU   string
}

type Config struct {
	AllocationType types.AllocationType
	UUID           string
	QuotaReq       float64
	QuotaLimit     float64
	MemoryReq      int64
	SMPartition    int //0-100

	NodeName         string
	VGPUUUID         string
	associatedGpu    *GPUInfo
	RequiredReplica  int
	SatisfiableRPS   float64
	QpsPerReplica    float64
	Cost             float64
	remainingRPS     float64 //negative if exceeds
	AllocatedReplica int
	AllocatedRPS     float64
	shelfItems       map[int]bool
	efficiencyScore  float64
}

func (c *Config) String() string {
	return fmt.Sprintf("Config: %v, UUID: %s, QuotaReq: %f, QuotaLimit: %f, MemoryReq: %d, SMPartition: %d, NodeName: %s, VGPUUUID: %s, associatedGpu: %v, RequiredReplica: %d, SatisfiableRPS: %f, QpsPerReplica: %f, Cost: %f, remainingRPS: %f, AllocatedReplica: %d, AllocatedRPS: %f, shelfItems: %v", c.AllocationType, c.UUID, c.QuotaReq, c.QuotaLimit, c.MemoryReq, c.SMPartition, c.NodeName, c.VGPUUUID, c.associatedGpu, c.RequiredReplica, c.SatisfiableRPS, c.QpsPerReplica, c.Cost, c.remainingRPS, c.AllocatedReplica, c.AllocatedRPS, c.shelfItems)
}

//make config comparable witrh uuid

func (c *Config) Equal(other *Config) bool {
	return c.UUID == other.UUID
}

func (ctr *NodeManager) GetConfigs(req *ResourceRequest, initial bool) ([]*Config, error) {
	ctr.nodesMtx.Lock()
	defer ctr.nodesMtx.Unlock()
	//get required memory
	requiredMemory, err := GetModelMemory(req.ModelName)
	if err != nil {
		return nil, err
	}

	//get remaining required qps
	remainingRequiredQPS := req.QPS
	configs := []*Config{}

	gpuInfos := []*GPUInfo{}

	for _, node := range ctr.nodes {

		//check if live
		if node.Status != NodeReady {
			klog.Infof("Node %s is not ready skipping..", node.NodeName)
			continue
		}

		gpuInfos = append(gpuInfos, extractGPUSFromNode(node)...)
	}

	//if preferred gpu is set, filter out the gpus that are not the preferred gpu
	if req.PreferredGPU != "" {
		preferredGPUs := []*GPUInfo{}
		for _, gpu := range gpuInfos {
			//gpu type contains the gpu type name
			if strings.Contains(gpu.GPUType, req.PreferredGPU) {
				preferredGPUs = append(preferredGPUs, gpu)
			}
		}
		gpuInfos = preferredGPUs
	}

	gpuInfos = sortGPUInfos(gpuInfos, req, requiredMemory, initial)

	klog.Infof("Found %d GPUs", len(gpuInfos))
	for _, devInfo := range gpuInfos {

		//check memory fits
		if !devInfo.FitsMemory(requiredMemory) {
			klog.Infof("GPU %s does not fit memory", devInfo.UUID)
			continue
		}

		//if allocation type is exclusive,

		// if req.AllocationType == types.AllocationTypeExclusive {
		// 	//check if gpu is virtual  and add to config
		// 	if devInfo.virtual {
		// 		config, err := ctr.getConfigForExclusive(devInfo, req.ModelName, requiredMemory)
		// 		if err != nil {
		// 			klog.Infof("GPU %s does not have config: error %v", devInfo.UUID, err)
		// 			continue
		// 		}

		// 		configs = append(configs, config)
		// 		remainingRequiredQPS -= config.SatisfiableRPS
		// 		//if remainingRequiredQPS is negative, it means we have enough qps support so we can break
		// 		if remainingRequiredQPS <= 0 {
		// 			break
		// 		}

		// 		if config != nil {
		// 			configs = append(configs, config)
		// 		}
		// 		remainingRequiredQPS -= config.SatisfiableRPS
		// 		//if remainingRequiredQPS is negative, it means we have enough qps support so we can break
		// 		if remainingRequiredQPS <= 0 {
		// 			break
		// 		}

		// 	}

		// 	continue
		// }

		bestConfig := ctr.getConfig(devInfo, req.ModelName, remainingRequiredQPS, requiredMemory, req.AllocationType)

		if bestConfig != nil {
			remainingRequiredQPS -= bestConfig.SatisfiableRPS
			configs = append(configs, bestConfig)
		} else {
			klog.Infof("No config found for GPU %s", devInfo.UUID)
		}

		if remainingRequiredQPS <= 0 {
			break
		}
	}

	if len(configs) == 0 {
		return nil, fmt.Errorf("no suitable selection found")
	} else {
		klog.Infof("Picked %d configs with satisfied QPS %f and missing QPS %f", len(configs), req.QPS-remainingRequiredQPS, remainingRequiredQPS)
	}
	return configs, nil
}

// PrepareConfigsRequirements prepares the configs for scheduling, removes failed configs
func (ctr *NodeManager) PrepareConfigsRequirements(
	req *ResourceRequest,
	configs []*Config) []*Config {

	ctr.nodesMtx.Lock()
	defer ctr.nodesMtx.Unlock()

	var newConfigs []*Config

	for _, config := range configs {

		node, ok := ctr.nodes[config.associatedGpu.NodeName]
		if !ok {
			klog.Errorf("node %s not found", config.associatedGpu.NodeName)
			continue
		}
		var configGPUInNode *GPUInfo

		configGPUInNode = node.physicalGPUsMap[config.associatedGpu.UUID]

		if configGPUInNode == nil && !config.associatedGpu.virtual { //physical gpu
			//add to node
			node, ok := ctr.nodes[config.associatedGpu.NodeName]
			if !ok {
				klog.Errorf("node %s not found", config.associatedGpu.NodeName)
				continue
			}
			node.physicalGPUsMap[config.associatedGpu.UUID] = config.associatedGpu

			configGPUInNode = config.associatedGpu
		} else if configGPUInNode == nil && config.associatedGpu.virtual {
			//create gpu if virtual
			node, ok := ctr.nodes[config.associatedGpu.NodeName]
			if !ok {
				klog.Errorf("node %s not found", config.associatedGpu.NodeName)
				continue
			}

			ctx := context.Background()

			klog.Infof("node client: %v", node.GrpcClient)

			response, err := node.GrpcClient.RequestVirtualGPU(ctx, &seti.RequestVirtualGPURequest{
				UseMps:    req.AllocationType == types.AllocationTypeFastPod || req.AllocationType == types.AllocationTypeMPS,
				Profileid: config.associatedGpu.profileID,
			})

			if err != nil {
				klog.Errorf("failed to request virtual gpu: %v", err)
				continue
			}
			node.availableGPUs = response.AvailableVirtualGpus

			createdGPU := GPUInfo{
				NodeName:                config.associatedGpu.NodeName,
				UUID:                    response.ProvisionedGpu.Uuid,
				Mem:                     int64(response.ProvisionedGpu.MemoryBytes),
				TotalSMPercentage:       config.associatedGpu.TotalSMPercentage,
				SMAllocationGranularity: 5,
				GPUType:                 config.associatedGpu.GPUType,
				profileID:               config.associatedGpu.profileID,
				virtual:                 true,
				ParentUUID:              response.ProvisionedGpu.ParentUuid,
				costPerSecond:           config.associatedGpu.costPerSecond,
				Usage:                   shelf.NewShelf(config.associatedGpu.TotalSMPercentage),
			}

			node.physicalGPUsMap[createdGPU.UUID] = &createdGPU
			configGPUInNode = &createdGPU
			config.associatedGpu = configGPUInNode
		}

		//set values of config in gpu info

		//configGPUInNode.

		config.associatedGpu = configGPUInNode
		config.VGPUUUID = configGPUInNode.UUID

		//

		config, err := configGPUInNode.AllocateAndCommitConfig(config)
		if err != nil {
			klog.Errorf("failed to allocate and commit config: %v", err)

		}

		if config.AllocatedReplica > 0 {
			newConfigs = append(newConfigs, config)
		}
	}

	return newConfigs
}

func (ctr *NodeManager) getConfigForExclusive(gpu *GPUInfo, modelName string, memory int64) (*Config, error) {
	//for exclusive, no need to consider sm and quota

	//check if gpu is virtual
	if gpu.virtual {
		//check memory fits
		if !gpu.FitsMemory(memory) {
			return nil, fmt.Errorf("GPU %s: %s does not fit memory", gpu.Name, gpu.GPUType)
		}

		//get qps per replica
		totalQPS := ctr.qpsStore.PredictQPS(modelName, gpu.GetTypeShortName(), gpu.TotalSMPercentage, 1.0, gpu.SMAllocationGranularity)

		if totalQPS == 0 {
			return nil, fmt.Errorf("GPU %s: %s does not have qps per replica", gpu.Name, gpu.GPUType)
		}

		return &Config{
			UUID:             string(uuid.NewUUID()),
			shelfItems:       make(map[int]bool),
			MemoryReq:        memory,
			associatedGpu:    gpu,
			QuotaReq:         1.0,
			QuotaLimit:       1.0,
			SMPartition:      gpu.TotalSMPercentage,
			VGPUUUID:         gpu.UUID,
			NodeName:         gpu.NodeName,
			RequiredReplica:  1,
			QpsPerReplica:    totalQPS,
			SatisfiableRPS:   totalQPS,
			AllocatedReplica: 1,
			AllocatedRPS:     totalQPS,

			remainingRPS:   0,
			Cost:           float64(gpu.costPerSecond) * float64(gpu.TotalSMPercentage/100),
			AllocationType: types.AllocationTypeExclusive,
		}, nil
	}

	return nil, nil

}

func (nm *NodeManager) getConfig(devInfo *GPUInfo, modelName string, remainingRequiredQPS float64, requiredMemory int64, allocationType types.AllocationType) *Config {
	if remainingRequiredQPS <= 0 {
		return nil
	}

	type candidateConfig struct {
		config     *Config
		efficiency float64
		replicas   float64
	}

	var candidates []candidateConfig
	quotaStartFrom := 0.1

	if allocationType == types.AllocationTypeExclusive {
		quotaStartFrom = 1.0
	}
	//TOtaosmperventage
	klog.Infof("Total SM Percentage: %d", devInfo.TotalSMPercentage)
	for sm := 1; sm <= devInfo.TotalSMPercentage; sm += 1 {
		for quota := quotaStartFrom; quota <= 1.0; quota += 0.1 {
			canFit, err := devInfo.Fits(sm, quota, requiredMemory)
			if err != nil {
				klog.Infof("Error checking if gpu %s can fit: %v", devInfo.UUID, err)
				continue
			}
			if !canFit {
				klog.Infof("GPU %s does not fit memory", devInfo.UUID)
				memoryRequiredAvailable := devInfo.AvailableMemory()
				klog.Infof("GPU %s has %d memory required, %d memory available", devInfo.UUID, requiredMemory, memoryRequiredAvailable)
				if memoryRequiredAvailable < requiredMemory {
					klog.Infof("GPU %s does not have enough memory", devInfo.UUID)
					continue
				}
				continue
			}

			qpsPerReplica := nm.qpsStore.PredictQPS(modelName, devInfo.GetTypeShortName(), sm, quota, 0)

			klog.Infof("QPS Per Replica: %f for sm: %d, quota: %f", qpsPerReplica, sm, quota)
			if qpsPerReplica == 0 {
				continue
			}

			possibleReplicasBySM := devInfo.Usage.MaxInsertableItems(quota, sm)
			possibleReplicasByMemory := math.Floor(float64(devInfo.AvailableMemory()) / float64(requiredMemory))
			possibleReplicas := math.Min(float64(possibleReplicasBySM), float64(possibleReplicasByMemory))
			achievableQPS := qpsPerReplica * possibleReplicas

			//if finalQPS exceeds the limit, we need to reduce the replicas
			if achievableQPS > remainingRequiredQPS {
				possibleReplicas = math.Ceil(remainingRequiredQPS / qpsPerReplica)
				achievableQPS = qpsPerReplica * possibleReplicas
			}

			// Efficiency: QPS per (SM * Quota)
			efficiency := qpsPerReplica / (float64(sm) * quota)

			config := &Config{
				UUID:            string(uuid.NewUUID()),
				MemoryReq:       requiredMemory,
				shelfItems:      make(map[int]bool),
				associatedGpu:   devInfo,
				QuotaReq:        quota,
				QuotaLimit:      math.Min(1.0, quota+0.3),
				SMPartition:     sm,
				VGPUUUID:        devInfo.UUID,
				NodeName:        devInfo.NodeName,
				RequiredReplica: int(possibleReplicas),
				QpsPerReplica:   qpsPerReplica,
				SatisfiableRPS:  achievableQPS,
				remainingRPS:    remainingRequiredQPS - achievableQPS,
				Cost:            float64(devInfo.costPerSecond) * float64(sm) / 100 * quota * possibleReplicas,
				AllocationType:  allocationType,
			}
			candidates = append(candidates, candidateConfig{
				config:     config,
				efficiency: efficiency,
				replicas:   possibleReplicas,
			})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// Find min/max for normalization
	minEff, maxEff := math.MaxFloat64, -math.MaxFloat64
	minRep, maxRep := math.MaxFloat64, -math.MaxFloat64
	for _, c := range candidates {
		if c.efficiency < minEff {
			minEff = c.efficiency
		}
		if c.efficiency > maxEff {
			maxEff = c.efficiency
		}
		if c.replicas < minRep {
			minRep = c.replicas
		}
		if c.replicas > maxRep {
			maxRep = c.replicas
		}
	}

	// Composite score and select best
	alpha, beta := 0.7, 0.3 // tune as needed
	bestScore := -math.MaxFloat64
	var best *Config
	for _, c := range candidates {
		var effNorm, repNorm float64
		if maxEff > minEff {
			effNorm = (c.efficiency - minEff) / (maxEff - minEff)
		} else {
			effNorm = 1.0
		}
		if maxRep > minRep {
			repNorm = (c.replicas - minRep) / (maxRep - minRep)
		} else {
			repNorm = 0.0
		}
		score := alpha*effNorm - beta*repNorm
		if score > bestScore {
			bestScore = score
			best = c.config
		}
	}
	return best
}

// normalizeScore returns a normalized score (higher is better) for two values, lower is better
func normalizeScore(val, other float64) float64 {
	max := math.Max(val, other)
	if max > 0 {
		return 1.0 - (val / max)
	}
	return 1.0
}

func extractGPUSFromNode(node *Node) []*GPUInfo {
	gpuInfos := []*GPUInfo{}

	allVGPU := node.availableGPUs

	//exlcude exclusive gpus
	for i, _ := range allVGPU {
		vgpu := allVGPU[i]
		var devInfo *GPUInfo
		var memBytes int64
		var uuid string
		var ok bool
		if vgpu.IsProvisioned && vgpu.ProvisionedGpu != nil {
			uuid = vgpu.ProvisionedGpu.Uuid
			memBytes = int64(vgpu.ProvisionedGpu.MemoryBytes)
			devInfo, ok = node.physicalGPUsMap[uuid]
			if !ok {
				devInfo = NewGPUDevInfo(
					node.NodeName,
					vgpu.PhysicalGpuType,
					false,
					nil,
					uuid,
					memBytes,
					int(vgpu.SmPercentage), 5)
			}
		} else {

			//unprovisioned
			memBytes = int64(vgpu.MemoryBytes)
			uuid = vgpu.Id
			devInfo = NewGPUDevInfo(
				node.NodeName,
				vgpu.PhysicalGpuType,
				true,
				vgpu.Profileid,
				uuid,
				memBytes,
				int(vgpu.SmPercentage), 10)
		}

		//exclude exclusive gpus
		if devInfo.allocationType != types.AllocationTypeExclusive {
			gpuInfos = append(gpuInfos, devInfo)
		}

	}

	return gpuInfos
}

// gpuScore calculates the weighted score for a GPU
func gpuScore(g *GPUInfo, other *GPUInfo, req *ResourceRequest, requestMemory int64, weights map[string]float64) float64 {

	costScore := normalizeScore(float64(g.costPerSecond), float64(other.costPerSecond))
	memoryDiff := float64(g.Mem - requestMemory)
	otherMemoryDiff := float64(other.Mem - requestMemory)
	memoryDiffScore := normalizeScore(memoryDiff, otherMemoryDiff)
	smScore := normalizeScore(float64(g.TotalSMPercentage), float64(other.TotalSMPercentage))
	memSizeScore := normalizeScore(float64(g.Mem), float64(other.Mem))
	typeMatchScore := 0.0
	if g.allocationType == req.AllocationType {
		typeMatchScore = 1.0
	}

	return weights["cost"]*costScore +
		weights["memoryDiff"]*memoryDiffScore +
		weights["sm"]*smScore +
		weights["memSize"]*memSizeScore +
		weights["typeMatch"]*typeMatchScore
}

func sortGPUInfos(gpuInfos []*GPUInfo, req *ResourceRequest, memory int64, initial bool) []*GPUInfo {

	var weights map[string]float64
	if req.AllocationType == types.AllocationTypeExclusive {

		weights = map[string]float64{
			"cost":       0.4,
			"memoryDiff": 0.3,
			"sm":         0.2,
			"memSize":    0.1,
			"typeMatch":  0.0,
		}
	} else {

		//if initial, prioritize cheaper GPUs
		costWeight := 0.4
		if initial {
			costWeight = 0.6
		}
		weights = map[string]float64{
			"cost":       costWeight,
			"memoryDiff": 0.0,
			"sm":         0.1,
			"memSize":    0.1,
			"typeMatch":  0.3,
		}
	}

	sort.Slice(gpuInfos, func(i, j int) bool {
		iScore := gpuScore(gpuInfos[i], gpuInfos[j], req, memory, weights)
		jScore := gpuScore(gpuInfos[j], gpuInfos[i], req, memory, weights)
		return iScore > jScore
	})

	return gpuInfos
}
