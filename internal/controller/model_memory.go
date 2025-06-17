package controller

import "fmt"

// in bytes
var modelMemoryMap map[string]int64 = map[string]int64{
	"resnet":      1_000_000_000, // ~1gb
	"bert":        2_073_741_824, // ~2GB
	"whispertiny": 1_000_000_000, // ~1GB

	"gnmt": 2_073_741_824, // ~2GB //deprecated
	"rnnt": 2_073_741_824, // ~2GB //deprecated
	"phi3": 4_000_000_000, // ~4GB for mini version //deprecated
}

func GetModelMemory(modelName string) (int64, error) {
	memory, ok := modelMemoryMap[modelName]
	if !ok {
		return 0, fmt.Errorf("model %s not found", modelName)
	}
	return memory, nil
}
