package controller

import (
	"context"

	fastpodv1 "github.com/KontonGu/FaST-GShare/pkg/apis/fastgshare.caps.in.tum/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

// reconcile the desired replicas of the FaSTFunc
func (r *FaSTFuncReconciler) AddFastPods(fastpods []*fastpodv1.FaSTPod) error {
	for _, fastpod := range fastpods {
		existed := &fastpodv1.FaSTPod{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: fastpod.GetName(), Namespace: fastpod.GetNamespace()}, existed)
		if err != nil {
			// if the FaSTPod is not created, create the FaSTod
			if errors.IsNotFound(err) {
				klog.Infof("TO create new FaSTPod %s with replica=%d.", fastpod.GetName(), *fastpod.Spec.Replicas)
				err = r.Create(context.TODO(), fastpod)
				if err != nil {
					klog.Errorf("Error Failed to create the FaSTPod %s.", fastpod.GetName())
					return err
				}
				return nil
			} else {
				klog.Errorf("Error failed to get the fastpod %s.", fastpod.GetName())
				return err
			}
		}
		existedCopy := existed.DeepCopy()
		replicas := int32(*fastpod.Spec.Replicas)
		existedCopy.Spec.Replicas = &replicas
		klog.Infof("Updating FaSTPod %s to have replicas %d.", fastpod.GetName(), replicas)
		err = r.Update(context.TODO(), existedCopy)
		if err != nil {
			klog.Errorf("Error failed to update the replicas of the FaSTPod %s.", fastpod.GetName())
			return err
		}
	}
	return nil
}
