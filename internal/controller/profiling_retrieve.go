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

package controller

func retrieveResource2RPSCapability(funcname string, quota float64, smPartition int64) float64 {
	// simple implementation, here should retrieve the store of the profiling
	// results with different temporal and spatial resource configuration
	// for the specific rps under the specific resource allocation
	// TODO
	return 30.0
}

func getMostEfficientConfig() (FaSTPodConfig, float64) {
	return FaSTPodConfig{30, 12, 1073741824, 1}, 50.0
}

type ProfileKey struct {
	ModelName    string
	GPUType      string
	SMPercentage int // use int for efficient hashing
	Quota        float64
}

type QPSStore struct {
	data map[ProfileKey]float64 // or int if QPS is always an integer
}

func NewQPSStore() *QPSStore {
	return &QPSStore{
		data: make(map[ProfileKey]float64),
	}
}

func (s *QPSStore) Set(modelName, gpuType string, smPercentage int, quota float64, qps float64) {
	key := ProfileKey{modelName, gpuType, smPercentage, quota}
	s.data[key] = qps
}

func (s *QPSStore) Get(modelName, gpuType string, smPercentage int, quota float64) (float64, bool) {
	key := ProfileKey{modelName, gpuType, smPercentage, quota}
	qps, exists := s.data[key]
	return qps, exists
}
