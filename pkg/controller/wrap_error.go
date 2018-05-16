package controller

import (
	"github.com/fengxsong/helm-crd/pkg/apis/helm.bitnami.com/v1"
	"github.com/golang/glog"
)

// wrapError only care about errors occur during installing or upgrading helm chart
type wrapError struct {
	obj *v1.HelmRelease
	err error
}

func (e *wrapError) Error() string {
	return e.err.Error()
}

func (c *Controller) handleWrapError(err *wrapError) {
	obj := err.obj.DeepCopy()
	obj.Status.Phase = v1.HelmRealeasePhaseFailed
	obj.Status.FailMsg = err.Error()
	if _, err := c.clientset.HelmV1().HelmReleases(obj.Namespace).Update(obj); err != nil {
		glog.Error(err.Error())
	}
}
