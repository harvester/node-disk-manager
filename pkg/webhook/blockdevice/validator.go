package blockdevice

import (
	"fmt"

	werror "github.com/harvester/webhook/pkg/error"
	"github.com/harvester/webhook/pkg/server/admission"
	ctlcorev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	ctlstoragev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/storage/v1"
	"github.com/sirupsen/logrus"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type Validator struct {
	admission.DefaultValidator

	BlockdeviceCache  ctldiskv1.BlockDeviceCache
	storageClassCache ctlstoragev1.StorageClassCache
	pvCache           ctlcorev1.PersistentVolumeCache
}

func NewBlockdeviceValidator(blockdeviceCache ctldiskv1.BlockDeviceCache, storageClassCache ctlstoragev1.StorageClassCache, pvCache ctlcorev1.PersistentVolumeCache) *Validator {
	return &Validator{
		BlockdeviceCache:  blockdeviceCache,
		storageClassCache: storageClassCache,
		pvCache:           pvCache,
	}
}

func (v *Validator) Create(_ *admission.Request, newObj runtime.Object) error {
	bd := newObj.(*diskv1.BlockDevice)
	if err := v.validateProvisioner(bd); err != nil {
		return err
	}
	return v.validateLVMProvisioner(nil, bd)
}

func (v *Validator) Update(_ *admission.Request, oldObj, newObj runtime.Object) error {
	newBd := newObj.(*diskv1.BlockDevice)
	oldBd := oldObj.(*diskv1.BlockDevice)
	if err := v.validateProvisioner(newBd); err != nil {
		return err
	}
	return v.validateLVMProvisioner(oldBd, newBd)
}

func (v *Validator) validateProvisioner(bd *diskv1.BlockDevice) error {
	if bd.Spec.Provisioner == nil {
		return nil
	}

	if bd.Spec.Provisioner.LVM != nil && bd.Spec.Provisioner.Longhorn != nil {
		return werror.NewBadRequest("Blockdevice should not have multiple provisioners")
	}
	return nil
}

// validateLVMProvisioner will check the block device with LVM provisioner and block
// if there is already have any pvc created with in the target volume group
func (v *Validator) validateLVMProvisioner(oldbd, newbd *diskv1.BlockDevice) error {

	// check again, skip if no LVM provisioner
	if newbd.Spec.Provisioner == nil || newbd.Spec.Provisioner.LVM == nil {
		return nil
	}

	// Adding case, should not happened
	if oldbd == nil {
		logrus.Info("Adding blockdevice with provisioner should not happen")
		return v.validateVGIsAlreadyUsed(newbd)
	}

	// means add or remove
	if oldbd.Spec.Provision != newbd.Spec.Provision {
		return v.validateVGIsAlreadyUsed(newbd)
	}

	return nil

}

func (v *Validator) validateVGIsAlreadyUsed(bd *diskv1.BlockDevice) error {
	targetVGName := bd.Spec.Provisioner.LVM.VgName
	// find what we wanted to check
	allStorageClasses, err := v.storageClassCache.List(labels.Everything())
	if err != nil {
		return werror.NewBadRequest("Failed to list storage classes")
	}
	targetSC := ""
	for _, sc := range allStorageClasses {
		if sc.Provisioner != utils.LVMCSIDriver {
			continue
		}
		scTargetNode := getLVMTopologyNodes(sc)
		if scTargetNode != bd.Spec.NodeName {
			continue
		}
		if sc.Parameters["vgName"] == targetVGName {
			targetSC = sc.Name
			break
		}
	}

	// no related SC found, just return
	if targetSC == "" {
		return nil
	}

	// check if there is any PV created with the targetSC
	pvs, err := v.pvCache.List(labels.Everything())
	if err != nil {
		return werror.NewBadRequest("Failed to list PVs")
	}
	for _, pv := range pvs {
		if pv.Spec.StorageClassName == targetSC {
			errStr := fmt.Sprintf("There is already a PVC created using the target volume group (%v), so we cannot add or remove the associated blockdevices", targetVGName)
			return werror.NewBadRequest(errStr)
		}
	}
	return nil
}

func (v *Validator) Resource() admission.Resource {
	return admission.Resource{
		Names:      []string{"blockdevices"},
		Scope:      admissionregv1.AllScopes,
		APIGroup:   diskv1.SchemeGroupVersion.Group,
		APIVersion: diskv1.SchemeGroupVersion.Version,
		ObjectType: &diskv1.BlockDevice{},
		OperationTypes: []admissionregv1.OperationType{
			admissionregv1.Create,
			admissionregv1.Update,
		},
	}
}

func getLVMTopologyNodes(sc *storagev1.StorageClass) string {
	for _, topology := range sc.AllowedTopologies {
		for _, matchLabel := range topology.MatchLabelExpressions {
			if matchLabel.Key == utils.LVMTopologyNodeKey {
				return matchLabel.Values[0]
			}
		}
	}
	return ""
}
