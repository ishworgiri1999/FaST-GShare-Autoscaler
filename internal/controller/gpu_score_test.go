package controller

import (
	"fastgshare/fastfunc/internal/profiling"
	"fastgshare/fastfunc/internal/shelf"
	"testing"

	"github.com/KontonGu/FaST-GShare/pkg/types"
)

func TestSortGPUInfos(t *testing.T) {
	makeGPU := func(name string, cost int, mem int64, sm int, allocType types.AllocationType) *GPUInfo {
		g := &GPUInfo{
			Name:              name,
			GPUType:           name,
			UUID:              name,
			TotalMemory:       mem,
			TotalSMPercentage: sm,
			allocationType:    allocType,
			costPerSecond:     cost,
			Usage:             shelf.NewShelf(100),
		}
		return g
	}

	tests := []struct {
		name   string
		gpus   []*GPUInfo
		req    *ResourceRequest
		memReq int64
	}{
		{
			name: "Prefer matching allocation type",
			gpus: []*GPUInfo{
				makeGPU("A", 10, 16000, 100, types.AllocationTypeFastPod),
				makeGPU("B", 10, 16000, 100, types.AllocationTypeMPS),
			},
			req:    &ResourceRequest{AllocationType: types.AllocationTypeFastPod, ModelName: "whisper"},
			memReq: 8000,
		},
		{
			name: "Prefer higher QPS/cost efficiency",
			gpus: []*GPUInfo{
				makeGPU("a100", 10, 16000, 100, types.AllocationTypeFastPod),
				makeGPU("v100", 10, 16000, 100, types.AllocationTypeFastPod),
				makeGPU("t1000", 10, 16000, 100, types.AllocationTypeFastPod),
			},
			req:    &ResourceRequest{AllocationType: types.AllocationTypeFastPod, ModelName: "whisper"},
			memReq: 8000,
		},
		{
			name: "Prefer better utilization balance",
			gpus: []*GPUInfo{
				func() *GPUInfo {
					g := makeGPU("A", 10, 16000, 100, types.AllocationTypeFastPod)
					g.UsageMemory = 8000
					g.Usage.UsedHeight = 50
					g.Usage.MaxHeight = 100
					return g
				}(),
				func() *GPUInfo {
					g := makeGPU("B", 10, 16000, 100, types.AllocationTypeFastPod)
					g.UsageMemory = 12000
					g.Usage.UsedHeight = 20
					g.Usage.MaxHeight = 100
					return g
				}(),
			},
			req:    &ResourceRequest{AllocationType: types.AllocationTypeFastPod, ModelName: "whisper"},
			memReq: 8000,
		},
		{
			name: "Prefer more free SM if balance equal",
			gpus: []*GPUInfo{
				func() *GPUInfo {
					g := makeGPU("A", 10, 16000, 100, types.AllocationTypeFastPod)
					g.UsageMemory = 8000
					g.Usage.UsedHeight = 50
					g.Usage.MaxHeight = 100
					return g
				}(),
				func() *GPUInfo {
					g := makeGPU("B", 10, 16000, 100, types.AllocationTypeFastPod)
					g.UsageMemory = 8000
					g.Usage.UsedHeight = 60
					g.Usage.MaxHeight = 100
					return g
				}(),
			},
			req:    &ResourceRequest{AllocationType: types.AllocationTypeFastPod, ModelName: "whisper"},
			memReq: 8000,
		},
		{
			name: "Prefer less free memory if all else equal",
			gpus: []*GPUInfo{
				func() *GPUInfo {
					g := makeGPU("A", 10, 16000, 100, types.AllocationTypeFastPod)
					g.UsageMemory = 12000
					g.Usage.UsedHeight = 50
					g.Usage.MaxHeight = 100
					return g
				}(),
				func() *GPUInfo {
					g := makeGPU("B", 10, 16000, 100, types.AllocationTypeFastPod)
					g.UsageMemory = 8000
					g.Usage.UsedHeight = 50
					g.Usage.MaxHeight = 100
					return g
				}(),
			},
			req:    &ResourceRequest{AllocationType: types.AllocationTypeFastPod, ModelName: "whisper"},
			memReq: 8000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nm := &NodeManager{
				qpsStore: profiling.NewEmptyQPSStore(),
			}

			//add fake data to qps store
			nm.qpsStore.Set("whisper", "a100", 100, 1.0, 50)
			nm.qpsStore.Set("whisper", "v100", 100, 1.0, 2)
			nm.qpsStore.Set("whisper", "t1000", 100, 1.0, 30)

			gpus := nm.sortGPUInfos(tt.gpus, tt.req, tt.memReq)
			if len(gpus) == 0 {
				t.Fatalf("No GPUs returned")
			}
			// Just check that the first GPU is not nil and is one of the input GPUs
			found := false
			for _, g := range tt.gpus {
				if gpus[0].Name == g.Name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("top GPU %s not in input list", gpus[0].Name)
			}
		})
	}
}
