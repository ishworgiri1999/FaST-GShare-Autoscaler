package controller

import (
	"math"
	"sort"
)

func (ctr *NodeManager) sortGPUInfos(gpuInfos []*GPUInfo, req *ResourceRequest, memory int64) []*GPUInfo {

	sort.Slice(gpuInfos, func(i, j int) bool {
		//sort by affinity first requeste type matches gpu type
		iTypeMatch := gpuInfos[i].allocationType == req.AllocationType
		jTypeMatch := gpuInfos[j].allocationType == req.AllocationType
		if iTypeMatch != jTypeMatch {
			return iTypeMatch // matching types always come first
		}
		qps := ctr.qpsStore.GetQPS(req.ModelName, gpuInfos[i].GetTypeShortName(), gpuInfos[i].TotalSMPercentage, 1.0, 0)
		qpsOther := ctr.qpsStore.GetQPS(req.ModelName, gpuInfos[j].GetTypeShortName(), gpuInfos[j].TotalSMPercentage, 1.0, 0)

		iEfficiency := qps / float64(gpuInfos[i].costPerSecond)
		jEfficiency := qpsOther / float64(gpuInfos[j].costPerSecond)
		if iEfficiency != jEfficiency {
			return iEfficiency > jEfficiency
		}

		// 3. Calculate actual utilization metrics
		iMemUtil := float64(gpuInfos[i].UsageMemory) / float64(gpuInfos[i].TotalMemory) // current memory utilization
		iSMUtil := float64(gpuInfos[i].Usage.UsedHeight) / float64(gpuInfos[i].Usage.MaxHeight)
		iBalance := math.Abs(iSMUtil - iMemUtil)

		jMemUtil := float64(gpuInfos[j].UsageMemory) / float64(gpuInfos[j].TotalMemory)
		jSMUtil := float64(gpuInfos[j].Usage.UsedHeight) / float64(gpuInfos[j].Usage.MaxHeight)
		jBalance := math.Abs(jSMUtil - jMemUtil)

		if iBalance != jBalance {
			return iBalance < jBalance
		}
		iFreeSM := float64(gpuInfos[i].Usage.MaxHeight - gpuInfos[i].Usage.UsedHeight)
		jFreeSM := float64(gpuInfos[j].Usage.MaxHeight - gpuInfos[j].Usage.UsedHeight)

		//prefer more free sm
		if iFreeSM != jFreeSM {
			return iFreeSM > jFreeSM
		}

		//memory utilization prefer less free memory
		iFreeMem := float64(gpuInfos[i].TotalMemory - gpuInfos[i].UsageMemory)
		jFreeMem := float64(gpuInfos[j].TotalMemory - gpuInfos[j].UsageMemory)

		return iFreeMem < jFreeMem

	})

	return gpuInfos
}
