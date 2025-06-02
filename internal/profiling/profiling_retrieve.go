/*
Copyright 2024 FaST-GShare Authors, KontonGu (Jianfeng Gu), et. al.
@Techinical University of Munich, CAPS Cloud Team

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package profiling

import (
	"encoding/csv"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"k8s.io/klog"
)

type ProfileKey struct {
	ModelName    string
	GPUType      string
	SMPercentage int // use int for efficient hashing
	Quota        float64
}

type RPSStore struct {
	data map[ProfileKey]float64 // or int if QPS is always an integer
}

func NewRPSStore() *RPSStore {
	//read from file system
	rpsStore := &RPSStore{
		data: make(map[ProfileKey]float64),
	}

	// List of CSV files to read
	csvFiles := []string{"profiling/a100.csv", "profiling/t1000.csv"} // Add more files as needed
	for _, csvFile := range csvFiles {
		f, err := os.Open(filepath.Clean(csvFile))
		//panif if fails
		if err != nil {
			log.Fatalf("Failed to open %s: %v", csvFile, err)
		}
		defer f.Close()
		reader := csv.NewReader(f)
		records, err := reader.ReadAll()
		if err != nil {
			log.Printf("Failed to read %s: %v", csvFile, err)
			continue
		}
		for i, rec := range records {
			if i == 0 {
				continue // skip header
			}
			if len(rec) < 5 {
				continue
			}
			gpuType := rec[0]
			modelName := rec[1]
			quota, err1 := strconv.ParseFloat(rec[2], 64)
			sm, err2 := strconv.Atoi(rec[3])
			qps, err3 := strconv.ParseFloat(rec[4], 64)
			if err1 != nil || err2 != nil || err3 != nil {
				continue
			}
			key := ProfileKey{modelName, gpuType, sm, quota}
			if qps <= 0 {
				continue
			}
			// Only set if new QPS is greater than existing
			if existing, ok := rpsStore.data[key]; !ok || qps > existing {
				rpsStore.data[key] = qps
			}
		}
	}
	klog.Infof("Initialized RPSStore with %d entries", len(rpsStore.data))
	// get one sample
	for key, qps := range rpsStore.data {
		klog.Infof("Sample: %v, QPS: %f", key, qps)
		break
	}
	return rpsStore
}

var RpsStore = NewRPSStore()

func (s *RPSStore) Set(modelName, gpuType string, smPercentage int, quota float64, qps float64) {
	if qps <= 0 {
		return
	}
	key := ProfileKey{modelName, gpuType, smPercentage, quota}
	s.data[key] = qps
}

func (s *RPSStore) Get(modelName, gpuTypeShortName string, smPercentage int, quota float64) (float64, bool) {
	key := ProfileKey{modelName, gpuTypeShortName, smPercentage, quota}
	qps, exists := s.data[key]
	if !exists || qps <= 0 {
		return 0, false
	}
	return qps, true
}

// PredictQPS estimates the QPS for the given parameters using bilinear interpolation or nearest neighbor fallback.
func (s *RPSStore) PredictQPS(modelName, gpuType string, smPercentage int, quota float64, roundBy int) float64 {
	// 1. Check for exact match
	if qps, exists := s.Get(modelName, gpuType, smPercentage, quota); exists {
		return qps
	}

	if roundBy != 0 {
		// Find the two nearest multiples of roundBy that bracket smPercentage
		lower := (smPercentage / roundBy) * roundBy
		upper := lower
		if smPercentage%roundBy != 0 {
			upper = lower + roundBy
		}

		var (
			qpsLower, foundLower = s.Get(modelName, gpuType, lower, quota)
			qpsUpper, foundUpper = s.Get(modelName, gpuType, upper, quota)
		)

		validLower := foundLower && qpsLower > 0
		validUpper := foundUpper && qpsUpper > 0

		if validLower && validUpper {
			return (qpsLower + qpsUpper) / 2.0
		} else if validLower {
			return qpsLower
		} else if validUpper {
			return qpsUpper
		}
	}

	// Fallback: return 0 if nothing found
	return 0
}
