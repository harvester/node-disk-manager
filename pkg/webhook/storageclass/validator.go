package storageclass

import (
	"fmt"

	werror "github.com/harvester/webhook/pkg/error"
	"github.com/harvester/webhook/pkg/server/admission"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/harvester/go-common/common"
	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type Validator struct {
	admission.DefaultValidator

	lvmVGCache ctldiskv1.LVMVolumeGroupCache
}

func NewStorageClassValidator(lvmVGCache ctldiskv1.LVMVolumeGroupCache) *Validator {
	return &Validator{
		lvmVGCache: lvmVGCache,
	}
}

func (v *Validator) Create(_ *admission.Request, newObj runtime.Object) error {
	sc := newObj.(*storagev1.StorageClass)
	return v.validateVGStatus(sc)
}

func (v *Validator) validateVGStatus(sc *storagev1.StorageClass) error {
	if sc.Provisioner != utils.LVMCSIDriver {
		return nil
	}
	vgs, err := v.lvmVGCache.List(common.HarvesterSystemNamespaceName, labels.Everything())
	if err != nil {
		return err
	}
	targetVGName := sc.Parameters["vgName"]

	for _, vg := range vgs {
		if vg.Spec.VgName != targetVGName {
			continue
		}
		if vg.Status == nil || vg.Status.Status != diskv1.VGStatusActive {
			errMsg := fmt.Sprintf("VG %s is not ready", vg.Spec.VgName)
			return werror.NewBadRequest(errMsg)
		}
	}
	return nil
}

func (v *Validator) Resource() admission.Resource {
	return admission.Resource{
		Names:      []string{"storageclasses"},
		Scope:      admissionregv1.ClusterScope,
		APIGroup:   storagev1.SchemeGroupVersion.Group,
		APIVersion: storagev1.SchemeGroupVersion.Version,
		ObjectType: &storagev1.StorageClass{},
		OperationTypes: []admissionregv1.OperationType{
			admissionregv1.Create,
		},
	}
}
