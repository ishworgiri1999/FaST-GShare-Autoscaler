package controller

import (
	"context"
	"fastgshare/fastfunc/internal/shelf"
	"fmt"
	"math"
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

	gpuInfos = ctr.sortGPUInfos(gpuInfos, req, requiredMemory)

	klog.Infof("Found %d GPUs", len(gpuInfos))
	for _, devInfo := range gpuInfos {

		//check memory fits
		if !devInfo.FitsMemory(requiredMemory) {
			klog.Infof("GPU %s does not fit memory", devInfo.UUID)
			continue
		}

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

func (ctr *NodeManager) FilterGPUs(gpuInfos []*GPUInfo, req *ResourceRequest, requiredMemory int64) []*GPUInfo {
	filteredGPUs := []*GPUInfo{}
	for _, gpu := range gpuInfos {

		isExclusive := gpu.allocationType == types.AllocationTypeExclusive

		isUsed := gpu.allocationType != types.AllocationTypeNone

		if req.AllocationType == types.AllocationTypeExclusive && isUsed {
			continue
		}

		if req.AllocationType == types.AllocationTypeFastPod && isExclusive {
			continue
		}

		if gpu.FitsMemory(requiredMemory) {
			filteredGPUs = append(filteredGPUs, gpu)
		}
	}
	return filteredGPUs
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
				TotalMemory:             int64(response.ProvisionedGpu.MemoryBytes),
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

func (nm *NodeManager) getConfig(devInfo *GPUInfo, modelName string, remainingRequiredQPS float64, requiredMemory int64, allocationType types.AllocationType) *Config {
	if remainingRequiredQPS <= 0 {
		return nil
	}
	var bestConfig *Config

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

			qpsPerReplica := nm.qpsStore.GetQPS(modelName, devInfo.GetTypeShortName(), sm, quota, 0)

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

			resourceUnits := float64(sm) * quota

			// Efficiency: QPS per (SM * Quota)
			efficiency := qpsPerReplica / resourceUnits

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
				efficiencyScore: efficiency,
			}

			if bestConfig == nil {
				bestConfig = config
			} else {
				//if efficiency score is higher, we should use this config
				if config.efficiencyScore > bestConfig.efficiencyScore {
					bestConfig = config
				}
			}

		}
	}

	return nil

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
