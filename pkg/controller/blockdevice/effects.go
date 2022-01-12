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
	onCmdTimeout(e, bd, func(done chan<- cmdResultUpdater) {
		logEffect(bd).Info("Start partition")
		cmdErr := disk.MakeGPTPartition(bd.Spec.DevPath)
		if cmdErr == nil {
			logEffect(bd).Info("Finish partitioning")
		} else {
			logEffect(bd).Errorf("Failed to partition: %v", cmdErr.Error())
		}

		done <- func(bd *diskv1.BlockDevice) *diskv1.BlockDevice {
			if cmdErr != nil {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDevicePartitionedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			} else {
				diskv1.ProvisionPhasePartitioned.Set(bd)
				setDevicePartitionedCondition(bd, corev1.ConditionTrue, "")
			}
			return bd
		}
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
	onCmdTimeout(e, bd, func(done chan<- cmdResultUpdater) {
		logEffect(bd).Info("Start formating")
		cmdErr := disk.MakeExt4DiskFormatting(bd.Spec.DevPath)
		if cmdErr == nil {
			logEffect(bd).Info("Finish formating")
		} else {
			logEffect(bd).Errorf("Failed to format: %v", cmdErr.Error())
		}

		done <- func(bd *diskv1.BlockDevice) *diskv1.BlockDevice {
			if cmdErr != nil {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDeviceFormattedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			} else {
				diskv1.ProvisionPhaseFormatted.Set(bd)
				setDeviceFormattedCondition(bd, corev1.ConditionTrue, "")
			}
			return bd
		}
	})
	return nil
}

// effectUnmountFilesystemFactory unmounts blockdevice from its mountPoint.
// Then update its ProvisionPhase back to ProvisionPhaseFormatted.
func effectUnmountFilesystemFactory(fs *block.FileSystemInfo) effect {
	return func(e effectController, bd *diskv1.BlockDevice) error {
		onCmdTimeout(e, bd, func(done chan<- cmdResultUpdater) {
			logEffect(bd).Infof("Start unmounting from %s", fs.MountPoint)
			cmdErr := disk.UmountDisk(fs.MountPoint)
			if cmdErr == nil {
				logEffect(bd).Infof("Finish unmounting from %s", fs.MountPoint)
			} else {
				logEffect(bd).Errorf("Failed to unmount from %s: %v", fs.MountPoint, cmdErr.Error())
			}

			done <- func(bd *diskv1.BlockDevice) *diskv1.BlockDevice {
				if cmdErr != nil {
					diskv1.ProvisionPhaseFailed.Set(bd)
					setDeviceMountedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
				} else {
					diskv1.ProvisionPhaseFormatted.Set(bd)
					setDeviceMountedCondition(bd, corev1.ConditionFalse, "")
				}
				return bd
			}
		})
		return nil
	}
}

// effectMountFilesystem mounts blockdevice onto its mountPoint.
// Then update its ProvisionPhase to ProvisionPhaseMounted.
func effectMountFilesystem(e effectController, bd *diskv1.BlockDevice) error {
	onCmdTimeout(e, bd, func(done chan<- cmdResultUpdater) {
		mountPoint := util.GetMountPoint(bd.Name)
		logEffect(bd).Infof("Start mounting onto %s", mountPoint)
		cmdErr := disk.MountDisk(bd.Spec.DevPath, mountPoint)
		if cmdErr == nil {
			logEffect(bd).Infof("Finish mounting onto %s", mountPoint)
		} else {
			logEffect(bd).Errorf("Failed to mount onto %s: %v", mountPoint, cmdErr.Error())
		}

		done <- func(bd *diskv1.BlockDevice) *diskv1.BlockDevice {
			if cmdErr != nil {
				diskv1.ProvisionPhaseFailed.Set(bd)
				setDeviceMountedCondition(bd, corev1.ConditionFalse, cmdErr.Error())
			} else {
				diskv1.ProvisionPhaseMounted.Set(bd)
				setDeviceMountedCondition(bd, corev1.ConditionTrue, "")
			}
			return bd
		}
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
				logEffect(bd).Info("Finish provisioning")
			}

			bdCpy := bd.DeepCopy()
			diskv1.ProvisionPhaseProvisioned.Set(bdCpy)
			setDeviceProvisionedCondition(bdCpy, corev1.ConditionTrue, "")
			if !reflect.DeepEqual(bd.Status, bdCpy.Status) {
				if _, err := e.Blockdevices().Update(bdCpy); err != nil {
					if errors.IsConflict(err) {
						// Hit optimistic lock. Fetch latest resource for next poll.
						newBd, err := e.BlockdeviceCache().Get(bd.Namespace, bd.Name)
						if err != nil {
							// If a critical resource is missing, then abort this polling
							// because we cannot update its state anymore
							return errors.IsNotFound(err), err
						}
						bd = newBd
					}
					return false, err
				}
			}
			return true, nil
		})
		return nil
	}
}

// effectUnprovisionDeviceFactory unprovisions blockdevice from target longhorn and
// update ProvisionPhase back to ProvisionPhaseFormatted
func effectUnprovisionDeviceFactory(node *longhornv1.Node) effect {
	return func(e effectController, bd *diskv1.BlockDevice) error {
		onUnprovisionFinished := func(bd *diskv1.BlockDevice) (bool, error) {
			bdCpy := bd.DeepCopy()
			diskv1.ProvisionPhaseMounted.Set(bdCpy)
			setDeviceProvisionedCondition(bdCpy, corev1.ConditionFalse, "")
			if !reflect.DeepEqual(bd.Status, bdCpy.Status) {
				if _, err := e.Blockdevices().Update(bdCpy); err != nil {
					if errors.IsConflict(err) {
						// Hit optimistic lock. Fetch latest resource for next poll.
						newBd, err := e.BlockdeviceCache().Get(bd.Namespace, bd.Name)
						if err != nil {
							// If a critical resource is missing, then abort this polling
							// because we cannot update its state anymore
							return errors.IsNotFound(err), err
						}
						bd = newBd
					}
					return false, err
				}
			}
			return true, nil
		}

		jitterPoll(e, bd, func() (bool, error) {
			diskToRemove, ok := node.Spec.Disks[bd.Name]
			if !ok {
				return onUnprovisionFinished(bd)
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
			return onUnprovisionFinished(bd)
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
	currentPhase := bd.Status.ProvisionPhase
	go func() {
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
		doneCh <- struct{}{}
	}()

	go func() {
		select {
		case <-time.After(effectDefaultTimeout):
			// Stop JitterUntil
			close(stopCh)
			onEffectTimeout(e, bd)
		case <-doneCh:
			logEffect(bd).Debugf("Invalidated timeout: finish %s in time", currentPhase)
		}
	}()
}

func onEffectTimeout(e effectController, bd *diskv1.BlockDevice) {
	err := fmt.Errorf("Timeout in phase %s", bd.Status.ProvisionPhase)
	logEffect(bd).Error(err)
	newBd, getErr := e.BlockdeviceCache().Get(bd.Namespace, bd.Name)
	if getErr != nil {
		logEffect(bd).Errorf("Failed to update after timeout in phase %s: %v", bd.Status.ProvisionPhase, getErr.Error())
		return
	}
	newBdCpy := newBd.DeepCopy()
	setDeviceFailedCondition(newBdCpy, corev1.ConditionTrue, err.Error())
	diskv1.ProvisionPhaseFailed.Set(newBdCpy)
	if _, err := e.Blockdevices().Update(newBdCpy); err != nil {
		logEffect(bd).Errorf("Failed to update after timeout in phase %s: %v", bd.Status.ProvisionPhase, err.Error())
	}
}

type cmdResultUpdater func(*diskv1.BlockDevice) *diskv1.BlockDevice

func onCmdTimeout(e effectController, bd *diskv1.BlockDevice, f func(chan<- cmdResultUpdater)) {
	doneCh := make(chan cmdResultUpdater, 1)
	go func() {
		select {
		case <-time.After(effectDefaultTimeout):
			onEffectTimeout(e, bd)
		case update := <-doneCh:
			if _, err := e.Blockdevices().Update(update(bd.DeepCopy())); err != nil {
				emitError := func(err error) {
					logEffect(bd).Errorf("Failed to update in phase %s: %v", bd.Status.ProvisionPhase, err.Error())
				}
				if !errors.IsConflict(err) {
					emitError(err)
					return
				}
				newBd, err := e.BlockdeviceCache().Get(bd.Namespace, bd.Name)
				if err != nil {
					emitError(err)
					return
				}
				if _, err := e.Blockdevices().Update(update(newBd.DeepCopy())); err != nil {
					emitError(err)
					return
				}
			}
		}
	}()
	go f(doneCh)
}
