package blockdevice

import (
	"fmt"
	"reflect"
	"time"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	lhtypes "github.com/longhorn/longhorn-manager/types"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/disk"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/util"
)

const (
	effectDefaultTimeout      = 5 * time.Minute
	effectDefaultPollInterval = 10 * time.Second
)

type effect func(effectController, *diskv1.BlockDevice) error

type effectController interface {
	Nodes() ctllonghornv1.NodeClient
	NodeCache() ctllonghornv1.NodeCache
	Blockdevices() ctldiskv1.BlockDeviceController
	BlockdeviceCache() ctldiskv1.BlockDeviceCache
}

// effectGptPartition makes a new GPT table with a single partitions on given
// blockdevice, and then turns ProvisionPhase to phasePartitioned.
func effectGptPartition(e effectController, bd *diskv1.BlockDevice) error {
	onCmdTimeout(e, bd, func(done chan<- blockDeviceUpdater) {
		var updater blockDeviceUpdater
		logEffect(bd).Info("Start partition")

		if cmdErr := disk.MakeGPTPartition(bd.Spec.DevPath); cmdErr != nil {
			logEffect(bd).Errorf("Failed to partition: %v", cmdErr.Error())
			updater = func(bd *diskv1.BlockDevice) {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDevicePartitionedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			}
		} else {
			updater = func(bd *diskv1.BlockDevice) {
				diskv1.ProvisionPhasePartitioned.Set(bd)
				setDevicePartitionedCondition(bd, corev1.ConditionTrue, "")
			}
		}

		done <- updater
	})
	return nil
}

// effectEnqueueCurrentPhase enqueues blockdevice for next change handling
func effectEnqueueCurrentPhase(e effectController, bd *diskv1.BlockDevice) error {
	logEffect(bd).Infof("Enqueue for %s", bd.Status.ProvisionPhase)
	e.Blockdevices().EnqueueAfter(bd.Namespace, bd.Name, enqueueDelay)
	return nil
}

// effectPrepareFormatPartitionFactory prepares required spec for child partition before start formating
func effectPrepareFormatPartitionFactory(childBd *diskv1.BlockDevice) effect {
	return func(e effectController, parentBd *diskv1.BlockDevice) error {
		logEffect(parentBd).Infof("Start preparing format partition for its child %s", childBd.Name)
		chilBdCpy := childBd.DeepCopy()
		chilBdCpy.Spec.FileSystem.Provisioned = parentBd.Spec.FileSystem.Provisioned
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
	onCmdTimeout(e, bd, func(done chan<- blockDeviceUpdater) {
		var updater blockDeviceUpdater
		logEffect(bd).Info("Start formating")

		if cmdErr := disk.MakeExt4DiskFormatting(bd.Spec.DevPath); cmdErr != nil {
			logEffect(bd).Errorf("Failed to format: %v", cmdErr.Error())
			updater = func(bd *diskv1.BlockDevice) {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDeviceFormattedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			}
		} else {
			updater = func(bd *diskv1.BlockDevice) {
				diskv1.ProvisionPhaseFormatted.Set(bd)
				setDeviceFormattedCondition(bd, corev1.ConditionTrue, "")
			}
		}

		done <- updater
	})
	return nil
}

// effectUnmountFilesystemFactory unmounts blockdevice from its mountPoint.
// Then update its ProvisionPhase back to ProvisionPhaseFormatted.
func effectUnmountFilesystemFactory(fs *block.FileSystemInfo) effect {
	return func(e effectController, bd *diskv1.BlockDevice) error {
		onCmdTimeout(e, bd, func(done chan<- blockDeviceUpdater) {
			var updater blockDeviceUpdater
			logEffect(bd).Infof("Start unmounting from %s", fs.MountPoint)

			if cmdErr := disk.UmountDisk(fs.MountPoint); cmdErr != nil {
				logEffect(bd).Errorf("Failed to unmount from %s: %v", fs.MountPoint, cmdErr.Error())
				updater = func(bd *diskv1.BlockDevice) {
					diskv1.ProvisionPhaseFailed.Set(bd)
					setDeviceMountedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
				}
			} else {
				updater = func(bd *diskv1.BlockDevice) {
					diskv1.ProvisionPhaseFormatted.Set(bd)
					setDeviceMountedCondition(bd, corev1.ConditionFalse, "")
				}
			}

			done <- updater
		})
		return nil
	}
}

// effectMountFilesystem mounts blockdevice onto its mountPoint.
// Then update its ProvisionPhase to ProvisionPhaseMounted.
func effectMountFilesystem(e effectController, bd *diskv1.BlockDevice) error {
	onCmdTimeout(e, bd, func(done chan<- blockDeviceUpdater) {
		var updater blockDeviceUpdater
		mountPoint := util.GetMountPoint(bd.Name)
		logEffect(bd).Infof("Start mounting onto %s", mountPoint)

		if cmdErr := disk.MountDisk(bd.Spec.DevPath, mountPoint); cmdErr != nil {
			logEffect(bd).Errorf("Failed to mount onto %s: %v", mountPoint, cmdErr.Error())
			updater = func(bd *diskv1.BlockDevice) {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDeviceMountedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			}
		} else {
			updater = func(bd *diskv1.BlockDevice) {
				diskv1.ProvisionPhaseMounted.Set(bd)
				setDeviceMountedCondition(bd, corev1.ConditionTrue, "")
			}
		}

		done <- updater
	})
	return nil
}

// effectProvisionDeviceFactory provisions blockdevice as a disk to target longhorn and
// update ProvisionPhase to ProvisionPhaseProvisioned
func effectProvisionDeviceFactory(node *longhornv1.Node) effect {
	return func(e effectController, bd *diskv1.BlockDevice) error {
		logEffect(bd).Info("Start provisioning")
		jitterPoll(e, bd, func() (bool, error) {
			oldDisk, ok := node.Spec.Disks[bd.Name]
			newDisk := lhtypes.DiskSpec{
				Path:              util.GetMountPoint(bd.Name),
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
					if errors.IsConflict(err) {
						// Hit optimistic lock. Fetch latest resource for next poll.
						newNode, err := getNode(e, bd)
						if err != nil {
							// If a critical resource is missing, then abort this polling
							// because we cannot update its state anymore
							return errors.IsNotFound(err), err
						}
						node = newNode
					}
					return false, err
				}
			}

			return updateBlockDevice(e, bd, func(bd *diskv1.BlockDevice) {
				diskv1.ProvisionPhaseProvisioned.Set(bd)
				setDeviceProvisionedCondition(bd, corev1.ConditionTrue, "")
			})
		})
		return nil
	}
}

// effectUnprovisionDeviceFactory unprovisions blockdevice from target longhorn and
// update ProvisionPhase back to ProvisionPhaseFormatted
func effectUnprovisionDeviceFactory(node *longhornv1.Node) effect {
	return func(e effectController, bd *diskv1.BlockDevice) error {
		jitterPoll(e, bd, func() (bool, error) {
			diskToRemove, ok := node.Spec.Disks[bd.Name]
			if !ok {
				return updateBlockDevice(e, bd, func(bd *diskv1.BlockDevice) {
					diskv1.ProvisionPhaseMounted.Set(bd)
					setDeviceProvisionedCondition(bd, corev1.ConditionFalse, "")
				})
			}

			isUnprovisioning := false
			for _, tag := range diskToRemove.Tags {
				if tag == util.DiskRemoveTag {
					isUnprovisioning = true
					break
				}
			}

			if !isUnprovisioning {
				logEffect(bd).Info("Start unprovisioning")
				diskToRemove.AllowScheduling = false
				diskToRemove.EvictionRequested = true
				diskToRemove.Tags = append(diskToRemove.Tags, util.DiskRemoveTag)
				nodeCpy := node.DeepCopy()
				nodeCpy.Spec.Disks[bd.Name] = diskToRemove
				newNode, err := e.Nodes().Update(nodeCpy)
				if err != nil {
					// If a critical resource is missing, then abort this polling
					// because we cannot update its state anymore
					return errors.IsNotFound(err), err
				}
				node = newNode
				return false, nil
			}

			if status, ok := node.Status.DiskStatus[bd.Name]; ok && len(status.ScheduledReplica) > 0 {
				logEffect(bd).Info("Still unprovisioning")
				return false, nil
			}

			nodeCpy := node.DeepCopy()
			delete(nodeCpy.Spec.Disks, bd.Name)
			if !reflect.DeepEqual(nodeCpy.Spec.Disks, node.Spec.Disks) {
				if _, err := e.Nodes().Update(nodeCpy); err != nil {
					if errors.IsConflict(err) {
						// Hit optimistic lock. Fetch latest resource for next poll.
						newNode, err := e.NodeCache().Get(node.Namespace, node.Name)
						if err != nil {
							// If a critical resource is missing, then abort this polling
							// because we cannot update its state anymore
							return errors.IsNotFound(err), err
						}
						node = newNode
					}
					return false, err
				}
			}
			// Finish. The device should back to mounted.
			return updateBlockDevice(e, bd, func(bd *diskv1.BlockDevice) {
				diskv1.ProvisionPhaseMounted.Set(bd)
				setDeviceProvisionedCondition(bd, corev1.ConditionFalse, "")
			})
		})
		return nil
	}
}

func getNode(e effectController, bd *diskv1.BlockDevice) (*longhornv1.Node, error) {
	node, err := e.NodeCache().Get(bd.Namespace, bd.Spec.NodeName)
	if err != nil && errors.IsNotFound(err) {
		node, err = e.Nodes().Get(bd.Namespace, bd.Spec.NodeName, metav1.GetOptions{})
	}
	return node, err
}

func logEffect(bd *diskv1.BlockDevice) *logrus.Entry {
	return logrus.WithFields(logrus.Fields{
		"device":  bd.Name,
		"context": "Effect",
	})
}

// jitterPoll polls the conditionFunc with time jittering.
// The conditionFunc here has a slight different semantic: if `done` is true,
// no matter there is an error with it or not, jitterPoll always ceases.
func jitterPoll(e effectController, bd *diskv1.BlockDevice, conditionFunc wait.ConditionFunc) {
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		jitterUntil(conditionFunc, e, bd, stopCh)
		doneCh <- struct{}{}
	}()

	go func() {
		select {
		case <-time.After(effectDefaultTimeout):
			// Stop JitterUntil
			close(stopCh)
			onEffectTimeout(e, bd)
		case <-doneCh:
			logEffect(bd).Debugf("Invalidated timeout: finish %s in time", bd.Status.ProvisionPhase)
		}
	}()
}

func onEffectTimeout(e effectController, bd *diskv1.BlockDevice) {
	err := fmt.Errorf("Timeout in phase %s", bd.Status.ProvisionPhase)
	logEffect(bd).Error(err)
	if _, updateErr := updateBlockDevice(e, bd, func(bd *diskv1.BlockDevice) {
		setDeviceFailedCondition(bd, corev1.ConditionTrue, err.Error())
		diskv1.ProvisionPhaseFailed.Set(bd)
	}); updateErr != nil {
		logEffect(bd).Errorf("Failed to update after timeout in phase %s: %v", bd.Status.ProvisionPhase, err.Error())
	}
}

type blockDeviceUpdater func(*diskv1.BlockDevice)

func onCmdTimeout(e effectController, bd *diskv1.BlockDevice, f func(chan<- blockDeviceUpdater)) {
	stopCh := make(chan struct{})
	doneCh := make(chan blockDeviceUpdater, 1)
	go func() {
		select {
		case <-time.After(effectDefaultTimeout):
			close(stopCh)
			onEffectTimeout(e, bd)
		case update := <-doneCh:
			jitterUntil(func() (bool, error) {
				return updateBlockDevice(e, bd, update)
			}, e, bd, stopCh)
		}
	}()
	go f(doneCh)
}

func jitterUntil(conditionFunc wait.ConditionFunc, e effectController, bd *diskv1.BlockDevice, stopCh chan struct{}) {
	currentPhase := bd.Status.ProvisionPhase
	wait.JitterUntil(func() {
		done, err := conditionFunc()
		if done {
			if err != nil {
				logEffect(bd).Errorf("Ceased phase %s with err: %v", currentPhase, err.Error())
			} else {
				logEffect(bd).Infof("Finished phase %s", currentPhase)
			}
			close(stopCh)
		} else {
			if err != nil {
				logEffect(bd).Errorf("Failed during phase %s: %v", currentPhase, err.Error())
			} else {
				logEffect(bd).Infof("Continue polling in phase %s", currentPhase)
			}
		}
	}, effectDefaultPollInterval, 1.0, false, stopCh)
}

func updateBlockDevice(e effectController, bd *diskv1.BlockDevice, update blockDeviceUpdater) (bool, error) {
	bd, err := e.BlockdeviceCache().Get(bd.Namespace, bd.Name)
	if err != nil && errors.IsNotFound(err) {
		bd, err = e.Blockdevices().Get(bd.Namespace, bd.Name, metav1.GetOptions{})
		if err != nil {
			return errors.IsNotFound(err), err
		}
	}
	bdCpy := bd.DeepCopy()
	update(bdCpy)
	if !reflect.DeepEqual(bd, bdCpy) {
		if _, err := e.Blockdevices().Update(bdCpy); err != nil {
			return errors.IsNotFound(err), err
		}
	}
	return true, nil
}
