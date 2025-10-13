package blockdevice

import (
	"fmt"
	"strings"

	lhv1beta2 "github.com/harvester/harvester/pkg/generated/controllers/longhorn.io/v1beta2"
	werror "github.com/harvester/webhook/pkg/error"
	"github.com/harvester/webhook/pkg/server/admission"
	lhv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
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

const (
	BackingImageByDiskUUID = "longhorn.io/backingimage-by-diskuuid"
	NodeByBlockDeviceName  = "longhorn.io/node-by-blockdevice-name"
	ReplicaByDiskUUID      = "longhorn.io/replica-by-disk-uuid"
	ReplicaByVolume        = "longhorn.io/replica-by-volume"
)

type Validator struct {
	admission.DefaultValidator

	BlockdeviceCache  ctldiskv1.BlockDeviceCache
	storageClassCache ctlstoragev1.StorageClassCache
	pvCache           ctlcorev1.PersistentVolumeCache
	volumeCache       lhv1beta2.VolumeCache
	backingImageCache lhv1beta2.BackingImageCache
	lhNodeCache       lhv1beta2.NodeCache
	replicaCache      lhv1beta2.ReplicaCache
}

func NewBlockdeviceValidator(blockdeviceCache ctldiskv1.BlockDeviceCache, storageClassCache ctlstoragev1.StorageClassCache,
	pvCache ctlcorev1.PersistentVolumeCache, volumeCache lhv1beta2.VolumeCache, backingImageCache lhv1beta2.BackingImageCache,
	lhNodeCache lhv1beta2.NodeCache, replicaCache lhv1beta2.ReplicaCache) *Validator {
	backingImageCache.AddIndexer(BackingImageByDiskUUID, backingImageByDiskUUIDIndexer)
	lhNodeCache.AddIndexer(NodeByBlockDeviceName, nodeByBlockDeviceNameIndexer)
	replicaCache.AddIndexer(ReplicaByDiskUUID, replicaByDiskUUIDIndexer)
	replicaCache.AddIndexer(ReplicaByVolume, replicaByVolumeIndexer)
	return &Validator{
		BlockdeviceCache:  blockdeviceCache,
		storageClassCache: storageClassCache,
		pvCache:           pvCache,
		volumeCache:       volumeCache,
		backingImageCache: backingImageCache,
		lhNodeCache:       lhNodeCache,
		replicaCache:      replicaCache,
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
	if err := v.validateLVMProvisioner(oldBd, newBd); err != nil {
		return err
	}
	return v.validateLHDisk(oldBd, newBd)
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

func (v *Validator) validateLHDisk(oldBd, newBd *diskv1.BlockDevice) error {
	if oldBd.Spec.Provisioner != nil && newBd.Spec.Provisioner != nil &&
		oldBd.Spec.Provisioner.Longhorn != nil && newBd.Spec.Provisioner.Longhorn != nil &&
		oldBd.Spec.Provision && !newBd.Spec.Provision {
		err := v.validateVolumes(oldBd)
		if err != nil {
			return err
		}
		err = v.validateBackingImages(oldBd)
		if err != nil {
			return err
		}
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

func (v *Validator) validateVolumes(old *diskv1.BlockDevice) error {
	lhNode, err := v.lhNodeCache.Get(utils.LonghornSystemNamespaceName, old.Spec.NodeName)
	if err != nil {
		errStr := fmt.Sprintf("Failed to get longhorn node %s: %s", old.Spec.NodeName, err.Error())
		return werror.NewBadRequest(errStr)
	}

	diskStatus, ok := lhNode.Status.DiskStatus[old.Name]
	if !ok || diskStatus == nil {
		return nil
	}

	targetDiskUUID := diskStatus.DiskUUID
	replicaObjs, err := v.replicaCache.GetByIndex(ReplicaByDiskUUID, targetDiskUUID)
	if err != nil {
		errStr := fmt.Sprintf("Failed to get replicas by disk UUID %s: %s", targetDiskUUID, err.Error())
		return werror.NewBadRequest(errStr)
	}
	if len(replicaObjs) == 0 {
		return nil
	}

	volumesToCheck := make(map[string]struct{})
	for _, replicaObj := range replicaObjs {
		volumesToCheck[replicaObj.Spec.VolumeName] = struct{}{}
	}

	var unsafeVolumes []string
	for volName := range volumesToCheck {
		replicaObjsForVolume, err := v.replicaCache.GetByIndex(ReplicaByVolume, volName)
		if err != nil {
			errStr := fmt.Sprintf("Failed to get replicas for volume %s from index: %s", volName, err.Error())
			return werror.NewBadRequest(errStr)
		}

		var healthyReplicaCount int
		var replicaOnTargetDiskIsHealthy bool
		for _, rep := range replicaObjsForVolume {
			if rep.Spec.FailedAt == "" {
				healthyReplicaCount++
				if rep.Spec.DiskID == targetDiskUUID {
					replicaOnTargetDiskIsHealthy = true
				}
			}
		}

		if healthyReplicaCount == 1 && replicaOnTargetDiskIsHealthy {
			unsafeVolumes = append(unsafeVolumes, volName)
		}
	}

	if len(unsafeVolumes) > 0 {
		errStr := fmt.Sprintf("Cannot remove disk %s because it hosts the only healthy replica for the following volumes: %s",
			old.Spec.DevPath, strings.Join(unsafeVolumes, ", "))
		return werror.NewBadRequest(errStr)
	}

	return nil
}

func (v *Validator) validateBackingImages(old *diskv1.BlockDevice) error {
	lhNodes, err := v.lhNodeCache.GetByIndex(NodeByBlockDeviceName, old.Name)
	if err != nil {
		errStr := fmt.Sprintf("Error looking up node by blockdevice name %s: %s", old.Name, err.Error())
		return werror.NewBadRequest(errStr)
	}
	if len(lhNodes) != 1 || lhNodes[0] == nil {
		return nil
	}

	lhNode := lhNodes[0]
	diskStatus, ok := lhNode.Status.DiskStatus[old.Name]
	if !ok || diskStatus.DiskUUID == "" {
		return nil
	}

	uuid := diskStatus.DiskUUID
	backingImages, err := v.backingImageCache.GetByIndex(BackingImageByDiskUUID, uuid)
	if err != nil {
		errStr := fmt.Sprintf("Error looking up backing images by disk UUID %s: %s", uuid, err.Error())
		return werror.NewBadRequest(errStr)
	}
	if len(backingImages) == 0 {
		return nil
	}

	var unsafeToRemoveBackingImages []string
	for _, backingImage := range backingImages {
		if backingImage == nil {
			continue
		}
		readyCount := 0
		var readyDiskUUID string
		for diskUUID, fileStatus := range backingImage.Status.DiskFileStatusMap {
			if fileStatus == nil {
				continue
			}
			if fileStatus.State == lhv1.BackingImageStateReady {
				readyCount++
				readyDiskUUID = diskUUID
			}
		}
		if readyCount == 1 && readyDiskUUID == uuid {
			unsafeToRemoveBackingImages = append(unsafeToRemoveBackingImages, backingImage.Name)
		}
	}

	if len(unsafeToRemoveBackingImages) > 0 {
		errStr := fmt.Sprintf("Cannot remove disk %s as it contains the only ready copy for the following backing images: %s",
			old.Name, strings.Join(unsafeToRemoveBackingImages, ", "))
		return werror.NewBadRequest(errStr)
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

func backingImageByDiskUUIDIndexer(bi *lhv1.BackingImage) ([]string, error) {
	if bi.Spec.DiskFileSpecMap == nil {
		return []string{}, nil
	}
	diskUUIDs := make([]string, 0, len(bi.Status.DiskFileStatusMap))
	for key := range bi.Status.DiskFileStatusMap {
		diskUUIDs = append(diskUUIDs, key)
	}
	return diskUUIDs, nil
}

func nodeByBlockDeviceNameIndexer(node *lhv1.Node) ([]string, error) {
	if node.Status.DiskStatus == nil {
		return []string{}, nil
	}
	blockDeviceNames := make([]string, 0, len(node.Status.DiskStatus))
	for key := range node.Status.DiskStatus {
		blockDeviceNames = append(blockDeviceNames, key)
	}
	return blockDeviceNames, nil
}

func replicaByDiskUUIDIndexer(replica *lhv1.Replica) ([]string, error) {
	if replica.Spec.DiskID == "" {
		return []string{}, nil
	}
	return []string{replica.Spec.DiskID}, nil
}

func replicaByVolumeIndexer(replica *lhv1.Replica) ([]string, error) {
	if replica.Spec.VolumeName == "" {
		return []string{}, nil
	}
	return []string{replica.Spec.VolumeName}, nil
}
