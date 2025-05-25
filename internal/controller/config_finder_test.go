package controller

import (
	"fastgshare/fastfunc/internal/profiling"
	"fastgshare/fastfunc/internal/shelf"
	"testing"

	"github.com/KontonGu/FaST-GShare/pkg/proto/seti/v1"

	"github.com/KontonGu/FaST-GShare/pkg/types"
)

// Mock QpsStore and GetModelMemory for testing
var originalQpsStore = profiling.RpsStore
var originalGetModelMemory = GetModelMemory

type mockQpsStoreType struct{}

func (m *mockQpsStoreType) Get(modelName, gpuType string, smPercentage int, quota float64) (float64, bool) {
	return 10.0, true // Always return 10 QPS per replica
}

func TestGetConfigs_Basic(t *testing.T) {
	// Set up QpsStore for our test model/GPU/SM/quota
	modelName := "resnet50"
	gpuType := "A100"
	sm := 100
	quota := 0.2
	profiling.RpsStore.Set(modelName, gpuType, sm, quota, 10.0) // 10 QPS per replica

	// Minimal ShelfPacker
	usage := shelf.NewShelf(100)

	// Minimal GPUDevInfo
	gpu := &GPUInfo{
		GPUType:                 gpuType,
		UUID:                    "gpu-1",
		Mem:                     102_000_000, // matches resnet50
		TotalSMPercentage:       sm,
		SMAllocationGranularity: 10,
		NodeName:                "node-1",
		Usage:                   usage,
		costPerSecond:           1,
	}

	// Set up node with availableGPUs and physicalGPUsMap
	vgpu := seti.VirtualGPU{
		IsProvisioned:   true,
		ProvisionedGpu:  &seti.GPU{Uuid: "gpu-1", MemoryBytes: 102_000_000},
		PhysicalGpuType: gpuType,
		MemoryBytes:     102_000_000,
		Id:              "gpu-1",
		SmPercentage:    int32(sm),
	}

	node := &Node{
		NodeName:        "node-1",
		Status:          NodeReady,
		availableGPUs:   []*seti.VirtualGPU{&vgpu},
		physicalGPUsMap: map[string]*GPUInfo{"gpu-1": gpu},
	}

	nm := &NodeManager{
		nodes: map[string]*Node{"node-1": node},
	}

	req := &ResourceRequest{
		ModelName:      modelName,
		QPS:            10,
		AllocationType: types.AllocationTypeMPS,
	}

	configs, err := nm.GetConfigs(req, true)
	if err != nil {
		t.Fatalf("GetConfigs returned error: %v", err)
	}
	if len(configs) == 0 {
		t.Fatalf("Expected at least one config, got 0")
	}
	if configs[0].SatisfiableRPS < 10 {
		t.Errorf("Expected SatisfiableQPS >= 10, got %f", configs[0].SatisfiableRPS)
	}
}
