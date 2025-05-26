package controller

import (
	"fastgshare/fastfunc/internal/shelf"
	"fmt"

	"github.com/KontonGu/FaST-GShare/pkg/types"
)

type GPUInfo struct {
	virtual                 bool    //can this be deleted
	profileID               *uint32 //profile id for virtual gpu
	smCount                 int     // number of SMs not to be confused with SMPartition
	allocationType          types.AllocationType
	GPUType                 string // GPU type, eg. V100-PCIE-16GB
	UUID                    string
	Mem                     int64
	Name                    string // could be different than GPUType or same
	ParentUUID              string // physical gpu uuid (different for mig gpu, same for physical gpu)
	TotalSMPercentage       int    // 0-100 // 100 for physical GPU . for mig gpu, it is the percentage of SMs.
	NodeName                string
	Usage                   *shelf.ShelfPacker
	SMAllocationGranularity int //sm cant be assigned arbitrary, it has to be a multiple of this

	// Usage of GPU Memory
	UsageMem int64
	// podList      *list.List //Fastpod or MPSPod
	//	ExclusivePod string

	costPerSecond int

	isModelPresent          map[string]bool
	podConfigToShelfItemsId map[string][]int
}

func (g *GPUInfo) AvailableMemory() int64 {
	return g.Mem - g.UsageMem
}

func (g *GPUInfo) AllocateAndCommitConfig(config *Config) (*Config, error) {

	//set gpu allocation type
	g.allocationType = config.AllocationType

	// Remove memory from usage
	g.UsageMem -= (config.MemoryReq * int64(config.RequiredReplica))

	successfulReplicas := 0
	for i := 0; i < config.RequiredReplica; i++ {
		id, err := g.Usage.Insert(config.QuotaReq, config.SMPartition)
		if err != nil {
			// Rollback memory usage for failed inserts
			g.UsageMem += (config.MemoryReq * int64(config.RequiredReplica-successfulReplicas))
			break
		}
		config.shelfItems[id] = true
		successfulReplicas++
	}

	config.AllocatedReplica = successfulReplicas
	config.AllocatedRPS = config.QpsPerReplica * float64(config.AllocatedReplica)

	if successfulReplicas < config.RequiredReplica {
		return config, fmt.Errorf("failed to insert replica: required %d, successful %d", config.RequiredReplica, successfulReplicas)
	}

	return config, nil
}

func (g *GPUInfo) DeallocateConfig(config *Config) bool {

	//for each replica, remove from shelf
	for id := range config.shelfItems {
		g.Usage.Remove(id)
		delete(config.shelfItems, id)
	}
	//remove memory from usage
	g.UsageMem += (config.MemoryReq * int64(config.AllocatedReplica))

	return g.Usage.IsEmpty()
}

func (g *GPUInfo) ReduceConfig(config *Config, newReplicaCount int) bool {

	replicaReduceCount := config.AllocatedReplica - newReplicaCount

	if replicaReduceCount == 0 {
		return g.Usage.IsEmpty()
	}

	//remove memory from usage
	g.UsageMem -= (config.MemoryReq * int64(replicaReduceCount))
	//remove shelf items

	removalCount := 0
	for shelfItemId := range config.shelfItems {
		//remove shelf item one by one until replicaReduceCount is reached
		if removalCount < replicaReduceCount {
			g.Usage.Remove(shelfItemId)
			delete(config.shelfItems, shelfItemId)
			removalCount++
		} else {
			break
		}
	}

	return g.Usage.IsEmpty()
}

func NewGPUDevInfo(nodeName string, gpuType string, virtual bool, profileID *uint32, uuid string, mem int64, totalSMPercentage int, smAllocationGranularity int) *GPUInfo {

	cost := GetCost(gpuType)
	gpuDevInfo := &GPUInfo{
		virtual:                 virtual,
		profileID:               profileID,
		GPUType:                 gpuType,
		UUID:                    uuid,
		NodeName:                nodeName,
		Mem:                     mem,
		UsageMem:                0,
		TotalSMPercentage:       totalSMPercentage,
		SMAllocationGranularity: smAllocationGranularity,
		Usage:                   shelf.NewShelf(totalSMPercentage),
		// podList:                 list.New(),
		podConfigToShelfItemsId: make(map[string][]int),
		costPerSecond:           cost,
	}

	return gpuDevInfo
}

func (g *GPUInfo) FitsMemory(memory int64) bool {
	return memory <= g.Mem
}

func (g *GPUInfo) FitsSM(smPercentage int) bool {
	return smPercentage <= g.TotalSMPercentage
}

func (g *GPUInfo) Fits(smPercentage int, quota float64, memory int64) (bool, error) {

	if smPercentage > 100 {
		return false, fmt.Errorf("smPercentage > 100")
	}

	if memory > g.UsageMem {
		return false, fmt.Errorf("memory %d is greater than GPU memory %d", memory, g.Mem)
	}

	if quota > 1 {
		return false, fmt.Errorf("quota %f is greater than 1", quota)
	}

	canFit, err := g.Usage.CanFit(quota, smPercentage)
	if err != nil {
		return false, err
	}
	if !canFit {
		return false, fmt.Errorf("usag heighte %d + smPercentage %d * quota %f > 1", g.Usage.UsedHeight, smPercentage, quota)
	}

	return true, nil
}

var costsPerSecondMap map[string]int = map[string]int{
	"NVIDIA A100-PCIE-40GB": 400,
	"NVIDIA T1000":          100,
}

func GetCost(gpuType string) int {
	cost, ok := costsPerSecondMap[gpuType]
	if !ok {
		return 10_000 // High cost for unknown GPU types
	}
	return cost
}
