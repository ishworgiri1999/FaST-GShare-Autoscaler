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

func retrieveResource2RPSCapability(funcname string, quota float64, smPartition int) float64 {
	// simple implementation, here should retrieve the store of the profiling
	// results with different temporal and spatial resource configuration
	// for the specific rps under the specific resource allocation
	// TODO
	return 30.0
}

func getMostEfficientConfig() (FaSTPodConfig, float64) {
	return FaSTPodConfig{30, 12, 1073741824, 1, "A100", "0", "node-1", false}, 50.0
}

var profileData map[ProfileKey]float64 = map[ProfileKey]float64{
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 100, Quota: 0.2}:  67.38591699444268,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 12, Quota: 0.2}:   67.38487886874576,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 24, Quota: 0.2}:   72.03457000513981,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 50, Quota: 0.2}:   69.11592316066034,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 6, Quota: 0.2}:    73.24971794613924,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 100, Quota: 0.2}: 67.76065544626069,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 60, Quota: 0.2}:   66.72138622165636,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 12, Quota: 0.2}:  67.50001862549811,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 80, Quota: 0.2}:   67.35384365392547,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 24, Quota: 0.2}:  69.6112123553603,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 100, Quota: 0.4}:  67.38608796400355,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 50, Quota: 0.2}:  68.01659107474543,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 12, Quota: 0.4}:   67.34010036564189,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 6, Quota: 0.2}:   68.09546311452709,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 24, Quota: 0.4}:   66.85794466573061,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 60, Quota: 0.2}:  70.84254072807164,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 50, Quota: 0.4}:   68.1197149113869,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 80, Quota: 0.2}:  68.44265851128335,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 6, Quota: 0.4}:    66.25068996181766,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 100, Quota: 0.4}: 67.24470684684101,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 60, Quota: 0.4}:   67.80882900078336,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 12, Quota: 0.4}:  68.90021950065555,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 80, Quota: 0.4}:   71.69473080833326,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 24, Quota: 0.4}:  69.72009380917116,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 100, Quota: 0.6}:  66.5190315698684,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 50, Quota: 0.4}:  67.4607461917182,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 12, Quota: 0.6}:   66.07943872242501,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 6, Quota: 0.4}:   66.61789253506679,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 24, Quota: 0.6}:   72.158986355285,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 60, Quota: 0.4}:  66.56439540615396,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 50, Quota: 0.6}:   67.26736352611034,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 80, Quota: 0.4}:  70.63607605734065,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 6, Quota: 0.6}:    67.83827177076708,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 100, Quota: 0.6}: 69.49901569244965,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 60, Quota: 0.6}:   68.56499798022966,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 12, Quota: 0.6}:  68.42946426956286,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 80, Quota: 0.6}:   67.72729871287433,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 24, Quota: 0.6}:  67.65987426066009,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 100, Quota: 0.8}:  67.38872527064373,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 50, Quota: 0.6}:  72.96325410136883,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 12, Quota: 0.8}:   68.06604507667126,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 6, Quota: 0.6}:   68.73210546362036,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 24, Quota: 0.8}:   68.7114908116919,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 60, Quota: 0.6}:  67.37720718301493,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 50, Quota: 0.8}:   67.43109561866876,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 80, Quota: 0.6}:  67.45811076126535,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 6, Quota: 0.8}:    68.80460234205607,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 100, Quota: 0.8}: 67.53351635240932,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 60, Quota: 0.8}:   71.84317072217841,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 12, Quota: 0.8}:  67.89364895584849,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 80, Quota: 0.8}:   67.67895786863248,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 24, Quota: 0.8}:  69.24518936451162,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 100, Quota: 1.0}:  69.62947806355189,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 50, Quota: 0.8}:  71.69248531765297,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 12, Quota: 1.0}:   67.31040838218475,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 6, Quota: 0.8}:   67.2648292512718,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 24, Quota: 1.0}:   66.42285647817423,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 60, Quota: 0.8}:  69.45926789528505,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 50, Quota: 1.0}:   69.86248778687866,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 80, Quota: 0.8}:  69.18287726299101,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 6, Quota: 1.0}:    67.29281700577022,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 100, Quota: 1.0}: 69.79367631564119,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 60, Quota: 1.0}:   66.36682183439518,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 12, Quota: 1.0}:  72.28704781807691,
	{ModelName: "rsnt", GPUType: "A100", SMPercentage: 80, Quota: 1.0}:   63.89443081068653,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 24, Quota: 1.0}:  67.56764920564156,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 50, Quota: 1.0}:  73.22939644730232,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 6, Quota: 1.0}:   68.21535183156237,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 60, Quota: 1.0}:  69.59549792920951,
	{ModelName: "rsnt", GPUType: "T1000", SMPercentage: 80, Quota: 1.0}:  67.87592090555836,
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

func NewQPSStore(data map[ProfileKey]float64) *QPSStore {
	return &QPSStore{
		data: data,
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

// PredictQPS estimates the QPS for the given parameters using bilinear interpolation or nearest neighbor fallback.
func (s *QPSStore) PredictQPS(modelName, gpuType string, smPercentage int, quota float64) float64 {
	// 1. Check for exact match
	if qps, exists := s.Get(modelName, gpuType, smPercentage, quota); exists {
		return qps
	}

	// 2. Gather all points for this model/gpu
	type point struct {
		sm  int
		q   float64
		val float64
	}
	var points []point
	for k, v := range s.data {
		if k.ModelName == modelName && k.GPUType == gpuType {
			points = append(points, point{k.SMPercentage, k.Quota, v})
		}
	}
	if len(points) == 0 {
		return 0 // or some default/fallback
	}

	// 3. Find the four surrounding points for bilinear interpolation
	var (
		smLow, smHigh       = -1, -1
		quotaLow, quotaHigh = -1.0, -1.0
	)
	for _, p := range points {
		if p.sm <= smPercentage && (smLow == -1 || p.sm > smLow) {
			smLow = p.sm
		}
		if p.sm >= smPercentage && (smHigh == -1 || p.sm < smHigh || smHigh == -1) {
			smHigh = p.sm
		}
		if p.q <= quota && (quotaLow == -1.0 || p.q > quotaLow) {
			quotaLow = p.q
		}
		if p.q >= quota && (quotaHigh == -1.0 || p.q < quotaHigh || quotaHigh == -1.0) {
			quotaHigh = p.q
		}
	}

	// 4. Try bilinear interpolation if all four corners exist
	get := func(sm int, q float64) (float64, bool) {
		for _, p := range points {
			if p.sm == sm && p.q == q {
				return p.val, true
			}
		}
		return 0, false
	}
	if smLow != -1 && smHigh != -1 && quotaLow != -1.0 && quotaHigh != -1.0 {
		q11, ok11 := get(smLow, quotaLow)
		q12, ok12 := get(smLow, quotaHigh)
		q21, ok21 := get(smHigh, quotaLow)
		q22, ok22 := get(smHigh, quotaHigh)
		if ok11 && ok12 && ok21 && ok22 && smHigh != smLow && quotaHigh != quotaLow {
			// Bilinear interpolation
			f1 := float64(smHigh-smPercentage) / float64(smHigh-smLow)
			f2 := float64(smPercentage-smLow) / float64(smHigh-smLow)
			qpsLow := q11*(quotaHigh-quota)/(quotaHigh-quotaLow) + q12*(quota-quotaLow)/(quotaHigh-quotaLow)
			qpsHigh := q21*(quotaHigh-quota)/(quotaHigh-quotaLow) + q22*(quota-quotaLow)/(quotaHigh-quotaLow)
			return qpsLow*f1 + qpsHigh*f2
		}
	}

	// 5. Fallback: nearest neighbor
	minDist := -1.0
	var bestQPS float64
	for _, p := range points {
		d := (float64(p.sm-smPercentage))*(float64(p.sm-smPercentage)) + (p.q-quota)*(p.q-quota)
		if minDist == -1.0 || d < minDist {
			minDist = d
			bestQPS = p.val
		}
	}
	return bestQPS
}
