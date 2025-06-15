package controller

import (
	"fastgshare/fastfunc/internal/profiling"
	"fastgshare/fastfunc/internal/shelf"
	"testing"

	"github.com/KontonGu/FaST-GShare/pkg/proto/seti/v1"

	"github.com/KontonGu/FaST-GShare/pkg/types"
)

var originalGetModelMemory = GetModelMemory

func TestGetConfigs_Basic(t *testing.T) {
	// Set up QpsStore for our test model/GPU/SM/quota
	rpsStore := profiling.NewEmptyQPSStore()

	for sm := 5; sm <= 100; sm += 5 {
		for _, quota := range []float64{0.2, 0.4, 0.6, 0.8, 1.0} {
			rpsStore.Set("resnet", "a100", sm, quota, 10.0)
		}
	}

	rpsStore.GetQPS("resnet", "a100", 100, 1.0, 5)

	// Minimal ShelfPacker
	usage := shelf.NewShelf(100)

	// Minimal GPUDevInfo
	gpu := &GPUInfo{
		GPUType:                 "A100",
		UUID:                    "gpu-1",
		TotalMemory:             2_000_000_000, // matches resnet (1GB)
		TotalSMPercentage:       100,
		SMAllocationGranularity: 5,
		NodeName:                "node-1",
		Usage:                   usage,
		Name:                    "A100",
		costPerSecond:           100,
	}

	// Set up node with availableGPUs and physicalGPUsMap
	vgpu := seti.VirtualGPU{
		IsProvisioned:   true,
		ProvisionedGpu:  &seti.GPU{Uuid: "gpu-1", Name: "a100", MemoryBytes: 2_000_000_000},
		PhysicalGpuType: "A100",
		MemoryBytes:     2_000_000_000,
		Id:              "gpu-1",
		SmPercentage:    int32(100),
	}

	node := &Node{
		NodeName:        "node-1",
		Status:          NodeReady,
		availableGPUs:   []*seti.VirtualGPU{&vgpu},
		physicalGPUsMap: map[string]*GPUInfo{"gpu-1": gpu},
	}

	nm := &NodeManager{
		nodes:    map[string]*Node{"node-1": node},
		qpsStore: rpsStore,
	}

	req := &ResourceRequest{
		ModelName:      "resnet",
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
	//replica should be 1
	if configs[0].RequiredReplica != 1 {
		t.Errorf("Expected AllocatedReplica = 1, got %d", configs[0].AllocatedReplica)
	}
}
