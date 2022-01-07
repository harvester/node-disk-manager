package blockdevice

import (
	"fmt"
	"reflect"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	lhtypes "github.com/longhorn/longhorn-manager/types"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/disk"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/util"
)

type effect func(effectController, *diskv1.BlockDevice) error

type effectController interface {
	Nodes() ctllonghornv1.NodeClient
	Blockdevices() ctldiskv1.BlockDeviceController
	BlockdeviceCache() ctldiskv1.BlockDeviceCache
}

// effectGptPartition makes a new GPT table with a single partitions on given
// blockdevice, and then turns ProvisionPhase to phasePartitioned.
func effectGptPartition(e effectController, bd *diskv1.BlockDevice) error {
	go func() {
		logEffect(bd).Info("Start partition")
		cmdErr := disk.MakeGPTPartition(bd.Spec.DevPath)
		if cmdErr == nil {
			logEffect(bd).Info("Finish partitioning")
		} else {
			logEffect(bd).Errorf("Failed to partition: %v", cmdErr.Error())
		}

		update := func(bd *diskv1.BlockDevice) *diskv1.BlockDevice {
			if cmdErr != nil {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDevicePartitionedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			} else {
				diskv1.ProvisionPhasePartitioned.Set(bd)
				setDevicePartitionedCondition(bd, corev1.ConditionTrue, "")
			}
			return bd
		}

		updateDevice(e, bd, update)
	}()
	return nil
}

// effectEnqueuePrepareFormatPartition enqueues effectPrepareFormatPartition
func effectEnqueuePrepareFormatPartition(e effectController, bd *diskv1.BlockDevice) error {
	logEffect(bd).Info("Enqueue for preparing format partition")
	e.Blockdevices().EnqueueAfter(bd.Namespace, bd.Name, enqueueDelay)
	return nil
}

// effectPrepareFormatPartition prepares required spec for child partition before start formating
func effectPrepareFormatPartition(childBd *diskv1.BlockDevice) effect {
	return func(e effectController, parentBd *diskv1.BlockDevice) error {
		logEffect(parentBd).Infof("Start preparing format partition for its child %s", childBd.Name)
		chilBdCpy := childBd.DeepCopy()
		chilBdCpy.Spec.FileSystem.MountPoint = parentBd.Spec.FileSystem.MountPoint
		chilBdCpy.Spec.FileSystem.ForceFormatted = true
		if !reflect.DeepEqual(chilBdCpy.Spec.FileSystem, childBd.Spec.FileSystem) {
			if _, err := e.Blockdevices().Update(chilBdCpy); err != nil {
				return err
			}
		}
		// cleanup filesystem spec of parent device
		parentBdCpy := parentBd.DeepCopy()
		parentBdCpy.Spec.FileSystem = &diskv1.FilesystemInfo{}
		if !reflect.DeepEqual(parentBd.Spec.FileSystem, parentBdCpy.Spec.FileSystem) {
			if _, err := e.Blockdevices().Update(parentBdCpy); err != nil {
				return err
			}
		}
		return nil
	}
}

// effectFormatPartition format given blockdevice to EXT4 filesystem, and then turns
// ProvisionPhase to phaseFormatted.
func effectFormatPartition(e effectController, bd *diskv1.BlockDevice) error {
	go func() {
		logEffect(bd).Info("Start formating")
		cmdErr := disk.MakeExt4DiskFormatting(bd.Spec.DevPath)
		if cmdErr == nil {
			logEffect(bd).Info("Finish formating")
		} else {
			logEffect(bd).Errorf("Failed to format: %v", cmdErr.Error())
		}

		update := func(bd *diskv1.BlockDevice) *diskv1.BlockDevice {
			if cmdErr != nil {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDeviceFormattedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			} else {
				diskv1.ProvisionPhaseFormatted.Set(bd)
				setDeviceFormattedCondition(bd, corev1.ConditionTrue, "")
			}
			return bd
		}

		updateDevice(e, bd, update)
	}()

	return nil
}

// effectUnmountFilesystem unmounts blockdevice from its mountPoint.
// Then update its ProvisionPhase back to ProvisionPhaseFormatted.
func effectUnmountFilesystem(fs *block.FileSystemInfo) effect {
	return func(e effectController, bd *diskv1.BlockDevice) error {
		logEffect(bd).Infof("Start unmounting from %s", fs.MountPoint)
		cmdErr := disk.UmountDisk(fs.MountPoint)
		if cmdErr == nil {
			logEffect(bd).Infof("Finish unmounting from %s", fs.MountPoint)
		} else {
			logEffect(bd).Errorf("Failed to unmount from %s: %v", fs.MountPoint, cmdErr.Error())
		}

		update := func(bd *diskv1.BlockDevice) *diskv1.BlockDevice {
			if cmdErr != nil {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDeviceMountedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			} else {
				diskv1.ProvisionPhaseFormatted.Set(bd)
				setDeviceMountedCondition(bd, corev1.ConditionFalse, "")
			}
			return bd
		}

		return updateDevice(e, bd, update)
	}
}

// effectMountFilesystem mounts blockdevice onto its mountPoint.
// Then update its ProvisionPhase to ProvisionPhaseMounted.
func effectMountFilesystem(e effectController, bd *diskv1.BlockDevice) error {
	mountPoint := bd.Spec.FileSystem.MountPoint
	logEffect(bd).Infof("Start mounting onto %s", mountPoint)
	cmdErr := disk.MountDisk(bd.Spec.DevPath, mountPoint)
	if cmdErr == nil {
		logEffect(bd).Infof("Finish mounting onto %s", mountPoint)
	} else {
		logEffect(bd).Errorf("Failed to mount onto %s: %v", mountPoint, cmdErr.Error())
	}

	update := func(bd *diskv1.BlockDevice) *diskv1.BlockDevice {
		if cmdErr != nil {
			diskv1.ProvisionPhaseFailed.Set(bd)
			setDeviceMountedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
		} else {
			diskv1.ProvisionPhaseMounted.Set(bd)
			setDeviceMountedCondition(bd, corev1.ConditionTrue, "")
		}
		return bd
	}

	return updateDevice(e, bd, update)
}

// effectProvisionDevice provisions blockdevice as a disk to target longhorn and
// update ProvisionPhase to ProvisionPhaseProvisioned
func effectProvisionDevice(node *longhornv1.Node) effect {
	return func(e effectController, bd *diskv1.BlockDevice) error {
		logEffect(bd).Infof("Start provisioning onto node %s", node.Name)
		oldDisk, ok := node.Spec.Disks[bd.Name]
		newDisk := lhtypes.DiskSpec{
			Path:              bd.Spec.FileSystem.MountPoint,
			AllowScheduling:   true,
			EvictionRequested: false,
			StorageReserved:   0,
			Tags:              []string{},
		}
		if ok && reflect.DeepEqual(oldDisk, newDisk) {
			logEffect(bd).Infof("Already provisioned onto node %s", node.Name)
		} else {
			nodeCpy := node.DeepCopy()
			nodeCpy.Spec.Disks[bd.Name] = newDisk
			if _, err := e.Nodes().Update(nodeCpy); err != nil {
				logEffect(bd).Errorf("Failed to provision onto node %s: %v", node.Name, err.Error())
				return err
			}
			logEffect(bd).Infof("Finish provisioning onto node %s", node.Name)
		}
		bdCpy := bd.DeepCopy()
		diskv1.ProvisionPhaseProvisioned.Set(bdCpy)
		setDeviceProvisionedCondition(bdCpy, corev1.ConditionTrue, "")
		_, err := e.Blockdevices().Update(bdCpy)
		return err
	}
}

// effectUnprovisionDevice unprovisions blockdevice from target longhorn and
// update ProvisionPhase back to ProvisionPhaseFormatted
func effectUnprovisionDevice(node *longhornv1.Node, diskToRemove lhtypes.DiskSpec) effect {
	return func(e effectController, bd *diskv1.BlockDevice) error {
		onUnprovisionFailed := func(bd *diskv1.BlockDevice, err error) error {
			setDeviceProvisionedCondition(bd, corev1.ConditionFalse, err.Error())
			diskv1.ProvisionPhaseFailed.Set(bd)
			_, err = e.Blockdevices().Update(bd)
			return err
		}

		isUnprovisioning := false
		for _, tag := range diskToRemove.Tags {
			if tag == util.DiskRemoveTag {
				isUnprovisioning = true
				break
			}
		}

		if !isUnprovisioning {
			logEffect(bd).Infof("Start unprovisioning from node %s", node.Name)
			diskToRemove.AllowScheduling = false
			diskToRemove.EvictionRequested = true
			diskToRemove.Tags = append(diskToRemove.Tags, util.DiskRemoveTag)
			nodeCpy := node.DeepCopy()
			nodeCpy.Spec.Disks[bd.Name] = diskToRemove
			var err error
			node, err = e.Nodes().Update(nodeCpy)
			if err != nil {
				return onUnprovisionFailed(bd.DeepCopy(), err)
			}
		}

		if status, ok := node.Status.DiskStatus[bd.Name]; !ok && len(status.ScheduledReplica) > 0 {
			logEffect(bd).Infof("Still unprovisioning from %s. Enqueuing...", node.Name)
			e.Blockdevices().EnqueueAfter(bd.Namespace, bd.Name, enqueueDelay)
			return nil
		}

		logEffect(bd).Infof("Finish unprovisioning from %s", node.Name)
		nodeCpy := node.DeepCopy()
		delete(nodeCpy.Spec.Disks, bd.Name)
		if _, err := e.Nodes().Update(nodeCpy); err != nil {
			return onUnprovisionFailed(bd.DeepCopy(), err)
		}
		// Finish. Set phase to ProvisionPhaseFormatted
		bdCpy := bd.DeepCopy()
		diskv1.ProvisionPhaseFormatted.Set(bdCpy)
		setDeviceProvisionedCondition(bdCpy, corev1.ConditionFalse, "")
		if !reflect.DeepEqual(bd.Status, bdCpy.Status) {
			_, err := e.Blockdevices().Update(bdCpy)
			return err
		}
		return nil
	}
}

// noop does nothing
func noop(_ effectController, _ *diskv1.BlockDevice) error {
	return nil
}

// updateDevice is for internal use only.
func updateDevice(e effectController, bd *diskv1.BlockDevice, update func(bd *diskv1.BlockDevice) *diskv1.BlockDevice) error {
	if _, err := e.Blockdevices().Update(update(bd.DeepCopy())); err != nil {
		emitError := func(err error) error {
			err = fmt.Errorf("Failed to update in phase %s: %v", bd.Status.ProvisionPhase, err.Error())
			logEffect(bd).Error(err)
			return err
		}
		if !errors.IsConflict(err) {
			return emitError(err)
		}
		bd, err := e.BlockdeviceCache().Get(bd.Namespace, bd.Name)
		if err != nil {
			return emitError(err)
		}
		if _, err := e.Blockdevices().Update(update(bd.DeepCopy())); err != nil {
			return emitError(err)
		}
	}
	return nil
}

func logEffect(bd *diskv1.BlockDevice) *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"device":  bd.Name,
		"context": "Effect",
	})
}
