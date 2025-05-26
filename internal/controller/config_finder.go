package controller

import (
	"context"
	"fastgshare/fastfunc/internal/profiling"
	"fastgshare/fastfunc/internal/shelf"
	"fmt"
	"math"
	"sort"

	"github.com/KontonGu/FaST-GShare/pkg/proto/seti/v1"
	"github.com/KontonGu/FaST-GShare/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/klog/v2"
)

type ResourceRequest struct {
	ModelName      string
	QPS            float64
	AllocationType types.AllocationType
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
			continue
		}

		gpuInfos = append(gpuInfos, extractGPUSFromNode(node)...)
	}

	gpuInfos = sortGPUInfos(gpuInfos, req, requiredMemory, initial)

	for _, devInfo := range gpuInfos {

		//check memory fits
		if !devInfo.FitsMemory(requiredMemory) {
			continue
		}

		//if allocation type is exclusive,

		if req.AllocationType == types.AllocationTypeExclusive {
			//check if gpu is virtual  and add to config
			if devInfo.virtual {
				config := getConfigForExclusive(devInfo, req.ModelName, remainingRequiredQPS, requiredMemory)
				if config != nil {
					configs = append(configs, config)
				}
				remainingRequiredQPS -= config.SatisfiableRPS
				//if remainingRequiredQPS is negative, it means we have enough qps support so we can break
				if remainingRequiredQPS <= 0 {
					break
				}

			}

			continue
		}

		bestConfig := getConfigForFastPod(devInfo, req.ModelName, remainingRequiredQPS, requiredMemory)

		//config from this gpu.

		if bestConfig != nil {
			remainingRequiredQPS -= bestConfig.SatisfiableRPS
			configs = append(configs, bestConfig)
		}

		if remainingRequiredQPS <= 0 {
			break
		}
	}

	klog.Infof("Picked %d configs with satisfied QPS %f and missing QPS %f", len(configs), req.QPS-remainingRequiredQPS, remainingRequiredQPS)

	if len(configs) == 0 {
		return nil, fmt.Errorf("no suitable selection found")
	}

	return configs, nil
}

// PrepareConfigsRequirements prepares the configs for scheduling, removes failed configs
func (ctr *NodeManager) PrepareConfigsRequirements(
	req *ResourceRequest,
	configs []*Config) []*Config {

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
				UUID:                    response.ProvisionedGpu.Name,
				Mem:                     int64(response.ProvisionedGpu.MemoryBytes),
				TotalSMPercentage:       config.associatedGpu.TotalSMPercentage,
				SMAllocationGranularity: 10,
				NodeName:                config.associatedGpu.NodeName,
				allocationType:          config.associatedGpu.allocationType,
				GPUType:                 config.associatedGpu.GPUType,
				profileID:               config.associatedGpu.profileID,
				virtual:                 true,
				ParentUUID:              response.ProvisionedGpu.Name,
				costPerSecond:           config.associatedGpu.costPerSecond,
				Usage:                   shelf.NewShelf(config.associatedGpu.TotalSMPercentage),
			}

			node.physicalGPUsMap[createdGPU.UUID] = &createdGPU
			configGPUInNode = &createdGPU
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

func getConfigForExclusive(gpu *GPUInfo, modelName string, remainingRequiredQPS float64, memory int64) *Config {
	//for exclusive, no need to consider sm and quota

	//check if gpu is virtual
	if gpu.virtual {
		//check memory fits
		if !gpu.FitsMemory(memory) {
			return nil
		}

		//get qps per replica
		totalQPS, ok := profiling.RpsStore.Get(modelName, gpu.GPUType, gpu.TotalSMPercentage, 1.0)
		if !ok {
			return nil
		}
		//only 1 replica is allowed
		if totalQPS > remainingRequiredQPS {
			return nil
		}

		return &Config{
			UUID:            string(uuid.NewUUID()),
			MemoryReq:       memory,
			associatedGpu:   gpu,
			QuotaReq:        1.0,
			QuotaLimit:      1.0,
			SMPartition:     gpu.TotalSMPercentage,
			VGPUUUID:        gpu.UUID,
			RequiredReplica: 1,
			QpsPerReplica:   totalQPS,
			SatisfiableRPS:  totalQPS,
			remainingRPS:    remainingRequiredQPS - totalQPS,
			Cost:            float64(gpu.costPerSecond) * float64(gpu.TotalSMPercentage/100),
			AllocationType:  types.AllocationTypeExclusive,
		}
	}

	return nil

}

func getConfigForFastPod(devInfo *GPUInfo, modelName string, remainingRequiredQPS float64, requiredMemory int64) *Config {

	var bestConfig *Config
	for sm := 10; sm <= devInfo.TotalSMPercentage; sm += devInfo.SMAllocationGranularity {
		for quota := 0.2; quota <= 1.0; quota += 0.2 {
			canFit, err := devInfo.Fits(sm, quota, requiredMemory)
			if err != nil {
				continue
			}
			if !canFit {
				continue
			}

			qpsPerReplica, ok := profiling.RpsStore.Get(modelName, devInfo.GPUType, sm, quota)
			if !ok {
				continue
			}

			if qpsPerReplica == 0 {
				continue
			}

			possibleReplicasBySM := devInfo.Usage.MaxInsertableItems(quota, sm)

			possibleReplicasByMemory := math.Floor(float64(devInfo.AvailableMemory()) / float64(requiredMemory))

			possibleReplicas := math.Min(float64(possibleReplicasBySM), float64(possibleReplicasByMemory))

			achiveableQPS := qpsPerReplica * possibleReplicas

			//if finalQPS exceeds the limit, we need to reduce the replicas
			if achiveableQPS > remainingRequiredQPS {
				possibleReplicas = math.Ceil(remainingRequiredQPS / qpsPerReplica)
				achiveableQPS = qpsPerReplica * possibleReplicas
			}

			cost := float64(devInfo.costPerSecond) * float64(sm/100) * quota * possibleReplicas

			if bestConfig == nil {
				bestConfig = &Config{
					UUID:            string(uuid.NewUUID()),
					MemoryReq:       requiredMemory,
					associatedGpu:   devInfo,
					QuotaReq:        quota,
					QuotaLimit:      math.Min(1.0, quota+0.3),
					SMPartition:     sm,
					VGPUUUID:        devInfo.UUID,
					NodeName:        devInfo.NodeName,
					RequiredReplica: int(possibleReplicas),
					QpsPerReplica:   qpsPerReplica,
					SatisfiableRPS:  achiveableQPS,
					remainingRPS:    remainingRequiredQPS - achiveableQPS,
					Cost:            cost,
					AllocationType:  types.AllocationTypeFastPod,
				}
			} else {
				better := achiveableQPS >= bestConfig.SatisfiableRPS
				oldExceeds := bestConfig.remainingRPS < 0
				newExceeds := remainingRequiredQPS-achiveableQPS < 0
				if oldExceeds && newExceeds && cost < bestConfig.Cost {
					better = true
				}
				if better {
					bestConfig = &Config{
						UUID:            string(uuid.NewUUID()),
						MemoryReq:       requiredMemory,
						associatedGpu:   devInfo,
						QuotaReq:        quota,
						QuotaLimit:      math.Min(1.0, quota+0.3),
						SMPartition:     sm,
						VGPUUUID:        devInfo.UUID,
						QpsPerReplica:   qpsPerReplica,
						NodeName:        devInfo.NodeName,
						RequiredReplica: int(possibleReplicas),
						SatisfiableRPS:  achiveableQPS,
						remainingRPS:    remainingRequiredQPS - achiveableQPS,
						Cost:            cost,
						AllocationType:  types.AllocationTypeFastPod,
					}
				}
			}
		}
	}

	return bestConfig

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
				devInfo = NewGPUDevInfo(vgpu.PhysicalGpuType, false, nil, uuid, memBytes, int(vgpu.SmPercentage), 10)
			}
		} else {
			//unprovisioned
			memBytes = int64(vgpu.MemoryBytes)
			uuid = vgpu.Id
			devInfo = NewGPUDevInfo(vgpu.PhysicalGpuType, true, vgpu.Profileid, uuid, memBytes, int(vgpu.SmPercentage), 10)
		}

		gpuInfos = append(gpuInfos, devInfo)

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
