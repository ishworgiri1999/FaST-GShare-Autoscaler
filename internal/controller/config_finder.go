package controller

import (
	"container/list"
	"fmt"

	"github.com/KontonGu/FaST-GShare/pkg/proto/seti/v1"
	"github.com/KontonGu/FaST-GShare/pkg/types"
	"k8s.io/klog/v2"
)

type ResourceRequest struct {
	qps            float64
	allocationType types.AllocationType
}

type SelectionResult struct {
	QuotaReq    float64
	QuotaLimit  float64
	SMPartition int //0-100
	NodeName    string
	VGPUUUID    string
}

func (ctr *NodeManager) FindBestNode(req *ResourceRequest) (*SelectionResult, error) {

	var bestNode *Node
	var bestVGPU *seti.VirtualGPU
	bestScore := 1e9 // Initialize to a large number
	var finalSM float64

	for _, n := range nodeList {
		node, ok := nodes[n.Name]

		if !ok {
			continue
		}
		//check if live
		if n, ok := nodesLiveness[n.Name]; ok && n.Status != NodeReady {
			continue
		}
		allVGPU := node.vgpus
		usageMap := node.vGPUID2GPU

		for _, vgpu := range allVGPU {
			var devInfo *GPUDevInfo
			var memBytes int64
			var uuid string

			if vgpu.IsProvisioned && vgpu.ProvisionedGpu != nil {
				uuid = vgpu.ProvisionedGpu.Uuid
				memBytes = int64(vgpu.ProvisionedGpu.MemoryBytes)
				devInfo, ok = usageMap[uuid]
				if !ok {
					devInfo = &GPUDevInfo{
						smCount:        int(vgpu.ProvisionedGpu.MultiprocessorCount),
						SMPercentage:   int(vgpu.SmPercentage),
						UUID:           uuid,
						Mem:            memBytes,
						Usage:          0,
						UsageMem:       0,
						FastPodList:    list.New(),
						MPSPodList:     list.New(),
						allocationType: types.AllocationTypeNone,
					}
				}
			} else {
				memBytes = int64(vgpu.MemoryBytes)
				uuid = vgpu.Id
				devInfo = &GPUDevInfo{
					smCount:        int(vgpu.MultiprocessorCount),
					SMPercentage:   int(vgpu.SmPercentage),
					UUID:           uuid,
					Mem:            memBytes,
					Usage:          0,
					UsageMem:       0,
					FastPodList:    list.New(),
					MPSPodList:     list.New(),
					allocationType: types.AllocationTypeNone,
				}
			}

			if memBytes == 0 {
				continue
			}

			klog.Infof("KONTON_TEST: gpu used sm usage = %f", devInfo.Usage)

			// Step 10: if not CanFit(G, R) then continue
			if !canFit(req, devInfo) {
				klog.Infof("KONTON_TEST: gpu cannot fit %f %f", devInfo.Usage, devInfo.UsageMem)
				continue
			}

			// Step 13: if R.gpu type != G.gpu type, adjust SMs
			var adjSM int
			var smRatio float64
			if req.AllocationType == types.AllocationTypeMPS {
				adjSM = 0
				smRatio = 0.0
			} else {
				if req.RequestedGPUType != nil && vgpu.ProvisionedGpu != nil && *req.RequestedGPUType != vgpu.ProvisionedGpu.Name {
					adjSM, err = TransformedSM(req, vgpu)
					if err != nil {
						klog.Errorf("error TransformedSM: %s", err)
						continue
					}
				} else if req.SMPercentage != nil {
					adjSM = *req.SMPercentage
				} else {
					adjSM = 0
				}
				if devInfo.smCount > 0 {
					smRatio = float64(adjSM) / float64(devInfo.smCount)
				} else {
					smRatio = 0.0
				}
			}

			// Step 19: mem ratio = R.mem req / G.total memory
			var memRatio float64
			if devInfo.Mem > 0 {
				memRatio = float64(req.Memory) / float64(devInfo.Mem)
			} else {
				memRatio = 0.0
			}

			// Affinity priority
			affinity_priority := 0.0
			gpuSet, ok := fastPodToPhysicalGPUs[req.podKey]
			klog.Infof("KONTON_TEST: gpuSet = %v", gpuSet)

			if ok && gpuSet[vgpu.ProvisionedGpu.ParentUuid] {
				klog.Infof("KONTON_TEST: gpu affinity priority = %f", affinity_priority)

				affinity_priority = 10.0
			} else {
				klog.Info("No affinity priority")
			}
			klog.Infof("KONTON_TEST: gpu affinity priority = %f", affinity_priority)

			// Mode priority
			mode_priority := 0.0
			if req.AllocationType == devInfo.allocationType && req.AllocationType != types.AllocationTypeExclusive {
				mode_priority = 3.0
			}

			// GPU priority
			gpu_priority := 0.0
			if req.RequestedGPUType != nil && vgpu.ProvisionedGpu != nil && *req.RequestedGPUType == vgpu.ProvisionedGpu.Name {
				gpu_priority = 1.0
			}

			// Step 22-27: scoring
			var score float64
			if req.AllocationType == types.AllocationTypeMPS || smRatio <= memRatio {
				// Mem-heavy: balance SMs (or always for MPS)
				score = (devInfo.Usage - float64(adjSM)) / 100
			} else {
				// SM-heavy: balance memory
				score = (float64((devInfo.Mem - devInfo.UsageMem) - req.Memory)) / float64(devInfo.Mem)
			}
			score = score - affinity_priority - mode_priority - gpu_priority

			klog.Infof("score for gpu %s , with parent %s is %f", vgpu.Id, vgpu.ProvisionedGpu.ParentUuid, score)

			if score < bestScore {
				bestScore = score
				bestNode = node
				bestVGPU = vgpu
				finalSM = float64(adjSM)
			}
		}
	}

	if bestNode != nil && bestVGPU != nil {
		// Allocation logic would go here (not implemented)
		klog.Infof("Selected GPU: %s on node %s with score %f and finalSM %f", bestVGPU.Id, bestNode.hostName, bestScore, finalSM)
		return &SelectionResult{fastPodKey: req.podKey, Node: bestNode, VGPU: bestVGPU, FinalSM: int(finalSM)}, nil
	}
	return nil, fmt.Errorf("no suitable candidates found")
}
