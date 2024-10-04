package blockdevice

import (
	"github.com/harvester/webhook/pkg/server/admission"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
)

type Mutator struct {
	admission.DefaultMutator

	BlockdeviceCache ctldiskv1.BlockDeviceCache
}

func NewBlockdeviceMutator(blockdeviceCache ctldiskv1.BlockDeviceCache) *Mutator {
	return &Mutator{
		BlockdeviceCache: blockdeviceCache,
	}
}

// Update is used to patch the filesystem.provisioned value to spec.provision
// We move the provision flag from Spec.Filesystem.Provision to Spec.Provision when we introduce the provisioner.
func (m *Mutator) Update(req *admission.Request, oldObj, newObj runtime.Object) (admission.Patch, error) {
	if req.IsFromController() {
		return nil, nil
	}
	var patchOps admission.Patch
	oldBd := oldObj.(*diskv1.BlockDevice)
	newBd := newObj.(*diskv1.BlockDevice)

	if newBd.Spec.FileSystem != nil && !newBd.Spec.FileSystem.Provisioned && !newBd.Spec.Provision {
		return nil, nil
	}

	prevProvision := oldBd.Status.ProvisionPhase == diskv1.ProvisionPhaseProvisioned
	// That case means the spec.filesystem.provisioned is deprecated, so keep the provision value
	if !prevProvision && newBd.Spec.Provision {
		return nil, nil
	}

	if !prevProvision && newBd.Spec.FileSystem.Provisioned {
		patchAddNewProvision := admission.PatchOp{
			Op:    admission.PatchOpReplace,
			Path:  "/spec/provision",
			Value: true,
		}
		patchOps = append(patchOps, patchAddNewProvision)
		if newBd.Spec.Provisioner == nil {
			provisionerVal := &diskv1.LonghornProvisionerInfo{
				EngineVersion: "LonghornV1",
			}
			patchAddNewProvisioner := admission.PatchOp{
				Op:    admission.PatchOpAdd,
				Path:  "/spec/provisioner",
				Value: provisionerVal,
			}
			patchOps = append(patchOps, patchAddNewProvisioner)
		}
		return patchOps, nil
	}
	// means we need to disable, align the .spec.filesystem.provisioned with .spec.provision -> false
	if prevProvision && !newBd.Spec.FileSystem.Provisioned {
		if newBd.Spec.Provision {
			patchProvision := admission.PatchOp{
				Op:    admission.PatchOpReplace,
				Path:  "/spec/provision",
				Value: false,
			}
			patchOps = append(patchOps, patchProvision)
		}
		return patchOps, nil
	}

	return patchOps, nil
}

func (m *Mutator) Resource() admission.Resource {
	return admission.Resource{
		Names:      []string{"blockdevices"},
		Scope:      admissionregv1.AllScopes,
		APIGroup:   diskv1.SchemeGroupVersion.Group,
		APIVersion: diskv1.SchemeGroupVersion.Version,
		ObjectType: &diskv1.BlockDevice{},
		OperationTypes: []admissionregv1.OperationType{
			// we donot care about Create because the bd is created by the controller
			admissionregv1.Update,
		},
	}
}
