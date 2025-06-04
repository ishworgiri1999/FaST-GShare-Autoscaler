package controller

import (
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
			Mem:               mem,
			TotalSMPercentage: sm,
			allocationType:    allocType,
			costPerSecond:     cost,
			Usage:             shelf.NewShelf(100),
		}
		return g
	}

	tests := []struct {
		name      string
		gpus      []*GPUInfo
		req       *ResourceRequest
		memReq    int64
		initial   bool
		expectTop string // name of the GPU expected to be first after sorting
	}{
		{
			name: "Prefer cheaper GPU",
			gpus: []*GPUInfo{
				makeGPU("A100", 12000, 40*1024, 100, types.AllocationTypeFastPod),
				makeGPU("T1000", 477, 16*1024, 100, types.AllocationTypeFastPod),
			},
			req: &ResourceRequest{
				ModelName:      "model",
				QPS:            1,
				AllocationType: types.AllocationTypeFastPod,
			},
			memReq:    8 * 1024,
			initial:   true,
			expectTop: "T1000",
		},
		{
			name: "Prefer more memory if cost equal",
			gpus: []*GPUInfo{
				makeGPU("T1000-1", 477, 8*1024, 100, types.AllocationTypeFastPod),
				makeGPU("T1000-2", 477, 16*1024, 100, types.AllocationTypeFastPod),
			},
			req: &ResourceRequest{
				ModelName:      "model",
				QPS:            1,
				AllocationType: types.AllocationTypeFastPod,
			},
			memReq:    4 * 1024,
			initial:   false,
			expectTop: "T1000-1",
		},
		{
			name: "Prefer more SM if cost and mem equal",
			gpus: []*GPUInfo{
				makeGPU("T1000-1", 477, 16*1024, 80, types.AllocationTypeFastPod),
				makeGPU("T1000-2", 477, 16*1024, 100, types.AllocationTypeFastPod),
			},
			req: &ResourceRequest{
				ModelName:      "model",
				QPS:            1,
				AllocationType: types.AllocationTypeFastPod,
			},
			memReq:    4 * 1024,
			initial:   false,
			expectTop: "T1000-1",
		},
		{
			name: "Prefer type match",
			gpus: []*GPUInfo{
				makeGPU("T1000", 477, 16*1024, 100, types.AllocationTypeMPS),
				makeGPU("A100", 12000, 40*1024, 100, types.AllocationTypeFastPod),
			},
			req: &ResourceRequest{
				ModelName:      "model",
				QPS:            1,
				AllocationType: types.AllocationTypeMPS,
			},
			memReq:    8 * 1024,
			initial:   false,
			expectTop: "T1000",
		},
		{
			name: "Exclusive: prefer cheaper",
			gpus: []*GPUInfo{
				makeGPU("A100", 12000, 40*1024, 100, types.AllocationTypeExclusive),
				makeGPU("T1000", 477, 16*1024, 100, types.AllocationTypeExclusive),
			},
			req: &ResourceRequest{
				ModelName:      "model",
				QPS:            1,
				AllocationType: types.AllocationTypeExclusive,
			},
			memReq:    8 * 1024,
			initial:   false,
			expectTop: "T1000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gpus := sortGPUInfos(tt.gpus, tt.req, tt.memReq, tt.initial)
			if len(gpus) == 0 {
				t.Fatalf("No GPUs returned")
			}
			if gpus[0].Name != tt.expectTop {
				t.Errorf("expected top GPU %s, got %s", tt.expectTop, gpus[0].Name)
			}
		})
	}
}
