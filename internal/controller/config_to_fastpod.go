package controller

import (
	fastfuncv1 "fastgshare/fastfunc/api/v1"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fastpodv1 "github.com/KontonGu/FaST-GShare/pkg/apis/fastgshare.caps.in.tum/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func getFastPodName(fastFuncName string, configUUID string) string {
	return fastFuncName + "-" + configUUID
}

// Based on Resource Configuration and create corresponding fastpods' specification for FaSTFunc `fastfunc`
func (r *FaSTFuncReconciler) ConvertConfigs2FaSTPods(fastfunc *fastfuncv1.FaSTFunc, configs []*Config) ([]*fastpodv1.FaSTPod, error) {
	fastpodlist := make([]*fastpodv1.FaSTPod, 0)
	for _, config := range configs {
		podSpec := corev1.PodSpec{}
		selector := metav1.LabelSelector{}

		fastfunc.Spec.PodSpec.DeepCopyInto(&podSpec)
		// if fastfunc.Spec.Selector != nil {
		// 	fastfunc.Spec.Selector.DeepCopyInto(&selector)
		// }

		extendedAnnotations := make(map[string]string)
		extendedLabels := make(map[string]string)

		// write the spec
		quota := fmt.Sprintf("%0.2f", config.QuotaReq)
		smPartition := strconv.Itoa(int(config.SMPartition))
		mem := strconv.Itoa(int(config.MemoryReq))
		extendedLabels["com.openfaas.scale.min"] = strconv.Itoa(int(config.AllocatedReplica))
		extendedLabels["com.openfaas.scale.max"] = strconv.Itoa(int(config.AllocatedReplica))
		extendedLabels["fast_function"] = fastfunc.ObjectMeta.Name
		extendedAnnotations[fastpodv1.FaSTGShareGPUQuotaRequest] = quota
		extendedAnnotations[fastpodv1.FaSTGShareGPUQuotaLimit] = fmt.Sprintf("%0.2f", config.QuotaLimit)
		extendedAnnotations[fastpodv1.FaSTGShareGPUSMPartition] = smPartition
		extendedAnnotations[fastpodv1.FaSTGShareGPUMemory] = mem
		extendedAnnotations[fastpodv1.FaSTGShareNodeName] = config.NodeName
		extendedAnnotations[fastpodv1.FaSTGShareVGPUID] = config.VGPUUUID
		extendedAnnotations[fastpodv1.FastGshareAllocationType] = string(config.AllocationType)
		extendedAnnotations["config_uuid"] = config.UUID
		extendedAnnotations["rps"] = fmt.Sprintf("%0.2f", config.AllocatedRPS)
		fixedReplica_int32 := int32(config.AllocatedReplica)
		fastpod := &fastpodv1.FaSTPod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        getFastPodName(fastfunc.ObjectMeta.Name, config.UUID),
				Namespace:   "fast-gshare-fn",
				Labels:      extendedLabels,
				Annotations: extendedAnnotations,
			},
			Spec: fastpodv1.FaSTPodSpec{
				Selector: &selector,
				PodSpec:  podSpec,
				Replicas: &fixedReplica_int32,
			},
		}
		// ToDo: SetControllerReference here is useless, as the controller delete svc upon trial completion
		// Add owner reference to the service so that it could be GC
		if err := controllerutil.SetControllerReference(fastfunc, fastpod, r.Scheme); err != nil {
			klog.Info("Error setting ownerref")
			return nil, err
		}
		fastpodlist = append(fastpodlist, fastpod)
	}
	return fastpodlist, nil
}
