package controller

// package controller

// import (
// 	"container/list"

// 	"github.com/KontonGu/FaST-GShare/pkg/types"
// )

// type GPUDevInfo struct {
// 	virtual        bool //can this be deleted
// 	smCount        int  // number of SMs not to be confused with SMPartition
// 	allocationType types.AllocationType
// 	GPUType        string // GPU type, eg. V100-PCIE-16GB
// 	UUID           string
// 	Mem            int64
// 	Name           string // could be different than GPUType or same
// 	ParentUUID     string // physical gpu uuid (different for mig gpu, same for physical gpu)
// 	SMPercentage   int    // 0-100 // 100 for physical GPU . for mig gpu, it is the percentage of SMs.

// 	// Usage of GPU resource, SM * QtRequest only for FastPod
// 	Usage float64
// 	// Usage of GPU Memory
// 	UsageMem     int64
// 	podList      *list.List //Fastpod or MPSPod
// 	ExclusivePod string
// }
