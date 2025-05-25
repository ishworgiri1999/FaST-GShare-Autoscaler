package controller

import (
	"container/list"
	"fastgshare/fastfunc/internal/shelf"
	"fmt"

	"github.com/KontonGu/FaST-GShare/pkg/types"
)

type GPUDevInfo struct {
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
	UsageMem     int64
	podList      *list.List //Fastpod or MPSPod
	ExclusivePod string

	costPerSecond int

	isModelPresent          map[string]bool
	podConfigToShelfItemsId map[string][]int
}

func (g *GPUDevInfo) AvailableMemory() int64 {
	return g.Mem - g.UsageMem
}

func (g *GPUDevInfo) AllocateAndCommitConfig(config *Config) (*Config, error) {
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
	config.AllocatedQPS = config.QpsPerReplica * float64(config.AllocatedReplica)

	if successfulReplicas < config.RequiredReplica {
		return nil, fmt.Errorf("failed to insert replica: required %d, successful %d", config.RequiredReplica, successfulReplicas)
	}

	return config, nil
}

func (g *GPUDevInfo) DeallocateConfig(config *Config) error {

	//for each replica, remove from shelf
	for id := range config.shelfItems {
		g.Usage.Remove(id)
		delete(config.shelfItems, id)
	}
	//remove memory from usage
	g.UsageMem += (config.MemoryReq * int64(config.AllocatedReplica))

	return nil
}

func NewGPUDevInfo(gpuType string, virtual bool, profileID *uint32, uuid string, mem int64, totalSMPercentage int, smAllocationGranularity int) *GPUDevInfo {

	cost := GetCost(gpuType)
	gpuDevInfo := &GPUDevInfo{
		virtual:                 virtual,
		profileID:               profileID,
		GPUType:                 gpuType,
		UUID:                    uuid,
		Mem:                     mem,
		UsageMem:                0,
		TotalSMPercentage:       totalSMPercentage,
		SMAllocationGranularity: smAllocationGranularity,
		Usage:                   shelf.NewShelf(totalSMPercentage),
		podList:                 list.New(),
		podConfigToShelfItemsId: make(map[string][]int),
		costPerSecond:           cost,
	}

	return gpuDevInfo
}

func (g *GPUDevInfo) FitsMemory(memory int64) bool {
	return memory <= g.Mem
}

func (g *GPUDevInfo) FitsSM(smPercentage int) bool {
	return smPercentage <= g.TotalSMPercentage
}

func (g *GPUDevInfo) Fits(smPercentage int, quota float64, memory int64) (bool, error) {

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
	"A100":  400, // Example cost per hour for A100 GPU (cents per hour)
	"T1000": 40,  // Example cost per hour for T1000 GPU (cents per hour)
}

func GetCost(gpuType string) int {
	cost, ok := costsPerSecondMap[gpuType]
	if !ok {
		return 10_000 // High cost for unknown GPU types
	}
	return cost
}
