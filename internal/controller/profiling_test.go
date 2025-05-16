package controller

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSampleSprintFunction(t *testing.T) {

	striner := "Hello, World!"
	fmt.Println(striner)

	assert.Equal(t, striner, "Hello, World!")
}

func TestQPSStore_SetAndGet(t *testing.T) {
	// data := profileData
	store := NewQPSStore(map[ProfileKey]float64{})
	modelName := "resnet50"
	gpuType := "A100"
	smPercentage := 80
	quota := 0.5
	qps := 123.45

	// Set a value and retrieve it
	store.Set(modelName, gpuType, smPercentage, quota, qps)
	got, exists := store.Get(modelName, gpuType, smPercentage, quota)
	if !exists {
		t.Fatalf("Expected key to exist after Set, but it does not")
	}
	if got != qps {
		t.Errorf("Expected QPS %v, got %v", qps, got)
	}

	// Try to get a value that does not exist
	_, exists = store.Get("othermodel", gpuType, smPercentage, quota)
	if exists {
		t.Errorf("Expected key to not exist for different modelName")
	}

	// Overwrite value and check
	newQPS := 200.0
	store.Set(modelName, gpuType, smPercentage, quota, newQPS)
	got, exists = store.Get(modelName, gpuType, smPercentage, quota)
	if !exists || got != newQPS {
		t.Errorf("Expected overwritten QPS %v, got %v", newQPS, got)
	}
}

func TestQPSStore_PredictQPS(t *testing.T) {
	data := profileData
	store := NewQPSStore(data)
	modelName := "resnet50"
	gpuType := "A100"
	smPercentage := 80
	quota := 0.5

	predictedQPS := store.PredictQPS(modelName, gpuType, smPercentage, quota)
	if predictedQPS != 123.45 {
		t.Errorf("Expected predicted QPS %v, got %v", 123.45, predictedQPS)
	}

	predictedQPS = store.PredictQPS("othermodel", gpuType, smPercentage, quota)
	if predictedQPS != 0 {
		t.Errorf("Expected predicted QPS %v, got %v", 0, predictedQPS)
	}
}
