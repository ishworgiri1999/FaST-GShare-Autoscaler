package controller

import (
	"fastgshare/fastfunc/internal/shelf"
	"fmt"
	"strings"

	"github.com/KontonGu/FaST-GShare/pkg/types"
)

type GPUInfo struct {
	virtual                 bool    //can this be deleted
	profileID               *uint32 //profile id for virtual gpu
	smCount                 int     // number of SMs not to be confused with SMPartition
	allocationType          types.AllocationType
	GPUType                 string // GPU type, eg. V100-PCIE-16GB
	UUID                    string
	TotalMemory             int64
	Name                    string // could be different than GPUType or same
	ParentUUID              string // physical gpu uuid (different for mig gpu, same for physical gpu)
	TotalSMPercentage       int    // 0-100 // 100 for physical GPU . for mig gpu, it is the percentage of SMs.
	NodeName                string
	Usage                   *shelf.ShelfPacker
	SMAllocationGranularity int   //sm cant be assigned arbitrary, it has to be a multiple of this
	UsageMemory             int64 // Usage of GPU Memory
	costPerSecond           int
}

func (g *GPUInfo) GetTypeShortName() string {

	if strings.Contains(g.GPUType, "A100") {
		return "a100"
	}

	if strings.Contains(g.GPUType, "T1000") {
		return "t1000"
	}

	if strings.Contains(g.GPUType, "V100") {
		return "v100"
	}

	if strings.Contains(g.GPUType, "2080 Ti") {
		return "rtx2080ti"
	}

	return ""
}

func (g *GPUInfo) AvailableMemory() int64 {
	return g.TotalMemory - g.UsageMemory
}

func (g *GPUInfo) AllocateAndCommitConfig(config *Config) (*Config, error) {

	//set gpu allocation type
	g.allocationType = config.AllocationType

	// Remove memory from usage
	g.UsageMemory -= (config.MemoryReq * int64(config.RequiredReplica))

	successfulReplicas := 0
	for i := 0; i < config.RequiredReplica; i++ {
		id, err := g.Usage.Insert(config.QuotaReq, config.SMPartition)
		if err != nil {
			// Rollback memory usage for failed inserts
			g.UsageMemory += (config.MemoryReq * int64(config.RequiredReplica-successfulReplicas))
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
	g.UsageMemory += (config.MemoryReq * int64(config.AllocatedReplica))

	return g.Usage.IsEmpty()
}

func (g *GPUInfo) ReduceConfig(config *Config, newReplicaCount int) bool {

	replicaReduceCount := config.AllocatedReplica - newReplicaCount

	if replicaReduceCount == 0 {
		return g.Usage.IsEmpty()
	}

	//remove memory from usage
	g.UsageMemory -= (config.MemoryReq * int64(replicaReduceCount))
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

	gpuDevInfo := &GPUInfo{
		virtual:                 virtual,
		profileID:               profileID,
		GPUType:                 gpuType,
		UUID:                    uuid,
		NodeName:                nodeName,
		TotalMemory:             mem,
		UsageMemory:             0,
		TotalSMPercentage:       totalSMPercentage,
		SMAllocationGranularity: smAllocationGranularity,
		Usage:                   shelf.NewShelf(totalSMPercentage),
	}
	gpuDevInfo.costPerSecond = GetCost(gpuDevInfo.GetTypeShortName())

	return gpuDevInfo
}

func (g *GPUInfo) FitsMemory(memory int64) bool {
	return memory <= g.TotalMemory
}

func (g *GPUInfo) FitsSM(smPercentage int) bool {
	return smPercentage <= g.TotalSMPercentage
}

func (g *GPUInfo) Fits(smPercentage int, quota float64, memory int64) (bool, error) {

	if smPercentage > 100 {
		return false, fmt.Errorf("smPercentage > 100")
	}

	if memory > g.AvailableMemory() {
		return false, fmt.Errorf("memory %d is greater than available GPU memory %d", memory, g.AvailableMemory())
	}

	if quota > 1 {
		return false, fmt.Errorf("quota %f is greater than 1", quota)
	}

	canFit, err := g.Usage.CanFit(quota, smPercentage)
	if err != nil {
		return false, err
	}
	if !canFit {
		return false, fmt.Errorf("usag heighte %d: smPercentage %d * quota %f > 1", g.Usage.UsedHeight, smPercentage, quota)
	}

	return true, nil
}

// mock price based on market price
var costsPerSecondMap map[string]int = map[string]int{
	"a100":      12000, //1200$ per second
	"t1000":     477,   //477$ per second
	"v100":      4500,  //4500$ per second
	"rtx2080ti": 1000,  //1000$ per second
}

func GetCost(shortName string) int {
	cost, ok := costsPerSecondMap[shortName]
	if !ok {
		return 10_000 // High cost for unknown GPU types
	}
	return cost
}
