package controller

import (
	"github.com/golang/glog"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	extclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/fengxsong/helm-crd/pkg/apis/helm.bitnami.com/v1"
)

func ensureCustomResource(extClientset extclientset.Interface) error {
	crd := &apiextensions.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "helmreleases." + v1.SchemeGroupVersion.Group,
		},
		Spec: apiextensions.CustomResourceDefinitionSpec{
			Group:   v1.SchemeGroupVersion.Group,
			Version: v1.SchemeGroupVersion.Version,
			Scope:   apiextensions.NamespaceScoped,
			Names: apiextensions.CustomResourceDefinitionNames{
				Plural:     "helmreleases",
				Singular:   "helmrelease",
				Kind:       "HelmRelease",
				ListKind:   "HelmReleaseList",
				ShortNames: []string{"hrl"},
			},
		},
	}
	_, err := extClientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if apierrors.IsAlreadyExists(err) {
		glog.Info("Skip the creation for CustomResourceDefinition HelmReleases because it has already been created")
		return nil
	}
	if err != nil {
		return err
	}
	glog.Info("Create CustomResourceDefinition HelmReleases successfully")
	return nil
}
