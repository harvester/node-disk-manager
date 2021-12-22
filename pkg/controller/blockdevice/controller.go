package blockdevice

import (
	"context"
	"reflect"
	"time"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	lhtypes "github.com/longhorn/longhorn-manager/types"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/disk"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/util"
)

const (
	blockDeviceHandlerName = "harvester-block-device-handler"
	enqueueDelay           = 10 * time.Second
)

type Controller struct {
	namespace string
	nodeName  string

	nodeCache ctllonghornv1.NodeCache
	nodes     ctllonghornv1.NodeClient

	blockdevices     ctldiskv1.BlockDeviceController
	blockdeviceCache ctldiskv1.BlockDeviceCache

	scanner *Scanner
}

// Register register the block device CRD controller
func Register(
	ctx context.Context,
	nodes ctllonghornv1.NodeController,
	bds ctldiskv1.BlockDeviceController,
	opt *option.Option,
	scanner *Scanner,
) error {
	controller := &Controller{
		namespace:        opt.Namespace,
		nodeName:         opt.NodeName,
		nodeCache:        nodes.Cache(),
		nodes:            nodes,
		blockdevices:     bds,
		blockdeviceCache: bds.Cache(),
		scanner:          scanner,
	}

	if err := controller.scanner.StartScanning(); err != nil {
		return err
	}

	bds.OnChange(ctx, blockDeviceHandlerName, controller.OnBlockDeviceChange)
	bds.OnRemove(ctx, blockDeviceHandlerName, controller.OnBlockDeviceDelete)
	return nil
}

func (c *Controller) OnBlockDeviceChange(key string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device == nil || device.DeletionTimestamp != nil || device.Spec.NodeName != c.nodeName {
		return nil, nil
	}

	switch device.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
		return c.onDiskChange(device)
	case diskv1.DeviceTypePart:
		return c.onPartitionChange(device)
	}

	return nil, nil
}

func (c *Controller) onDiskChange(device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	fs := *device.Spec.FileSystem

	var err error

	if !fs.ForceFormatted {
		// Disk only cares about force formatting itself to a single root partition.
		return nil, nil
	}

	if diskv1.DevicePartitioning.IsTrue(device) {
		logrus.Infof("Await GPT partition for %s", device.Name)
		return nil, nil
	}

	if diskv1.DevicePartitioned.IsTrue(device) {
		logrus.Infof("Finsihed GPT partition for %s", device.Name)
		devPath := util.GetDiskPartitionPath(device.Spec.DevPath, 1)
		part := c.scanner.BlockInfo.GetPartitionByDevPath(device.Spec.DevPath, devPath)
		name := block.GeneratePartitionGUID(part, c.nodeName)
		bd, err := c.blockdeviceCache.Get(device.Namespace, name)
		if err != nil {
			// TODO: Should consider not found??
			return nil, err
		}
		bdCpy := bd.DeepCopy()
		bdCpy.Spec.FileSystem.MountPoint = device.Spec.FileSystem.MountPoint
		bdCpy.Spec.FileSystem.ForceFormatted = true
		if reflect.DeepEqual(bdCpy.Spec.FileSystem, bd.Spec.FileSystem) {
			return nil, nil
		}
		return c.blockdevices.Update(bdCpy)
	}

	logrus.Infof("Start GPT partition for %s", device.Name)
	deviceCpy := device.DeepCopy()
	setDevicePartitioningCondition(deviceCpy, corev1.ConditionTrue, "")
	device, err = c.blockdevices.Update(deviceCpy)
	if err != nil {
		return nil, err
	}
	// TODO: unmount first?
	go c.makeGPTPartition(device)

	return nil, nil
}

func (c *Controller) onPartitionChange(device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	fs := *device.Spec.FileSystem

	var err error

	if diskv1.DeviceFormatting.IsTrue(device) {
		logrus.Infof("Await partition formating for %s", device.Name)
		return nil, nil
	}
	if diskv1.DeviceMounting.IsTrue(device) {
		logrus.Infof("Await partition mounting for %s", device.Name)
		return nil, nil
	}
	if diskv1.DeviceUnmounting.IsTrue(device) {
		logrus.Infof("Await partition unmounting for %s", device.Name)
		return nil, nil
	}
	if diskv1.DeviceUnprovisioning.IsTrue(device) {
		logrus.Infof("Await partition unprovisioning for %s", device.Name)
		// May enqueue device if disk on longhorn node haven't yet evicted all replicas
		c.unprovisionDevice(device.DeepCopy())
		return nil, nil
	}

	if fs.ForceFormatted && !diskv1.DeviceFormatted.IsTrue(device) {
		logrus.Infof("Start formatting for %s", device.Name)
		deviceCpy := device.DeepCopy()
		setDeviceFormattingCondition(deviceCpy, corev1.ConditionTrue, "")
		device, err = c.blockdevices.Update(deviceCpy)
		if err != nil {
			return nil, err
		}
		// TODO: umount first?
		go c.formatPartition(device)
		return nil, nil
	}

	filesystem := c.scanner.BlockInfo.GetFileSystemInfoByDevPath(device.Spec.DevPath)

	if fs.MountPoint != filesystem.MountPoint {
		deviceCpy := device.DeepCopy()
		if fs.MountPoint != "" {
			logrus.Infof("Start mounting %s on %s", device.Name, fs.MountPoint)
			setDeviceMountingCondition(deviceCpy, corev1.ConditionTrue, "")
			device, err = c.blockdevices.Update(deviceCpy)
			if err != nil {
				return nil, err
			}
			go c.updateFilesystemMount(device, filesystem)
			return nil, nil
		}
		if diskv1.DeviceProvisioned.IsTrue(device) {
			logrus.Infof("Start unprovisioing %s from %s", device.Name, fs.MountPoint)
			setDeviceUnprovisioningCondition(deviceCpy, corev1.ConditionTrue, "")
			device, err = c.blockdevices.Update(deviceCpy)
			if err != nil {
				return nil, err
			}
			err := c.unprovisionDevice(device.DeepCopy())
			return nil, err
		}

		logrus.Infof("Start unmounting %s on %s", device.Name, fs.MountPoint)
		setDeviceUnmountingCondition(deviceCpy, corev1.ConditionTrue, "")
		device, err = c.blockdevices.Update(deviceCpy)
		if err != nil {
			return nil, err
		}
		go c.unmountFilesystem(device, filesystem)
		return nil, nil
	}

	if fs.MountPoint != "" && !diskv1.DeviceProvisioned.IsTrue(device) {
		deviceCpy := device.DeepCopy()
		logrus.Infof("Start provisioning %s on %s", device.Name, fs.MountPoint)
		if err := c.provisionDevice(device, filesystem); err != nil {
			return nil, err
		}
		setDeviceProvisionedCondition(deviceCpy, corev1.ConditionTrue, "")
		_, err = c.blockdevices.Update(deviceCpy)
		if err != nil {
			return nil, err
		}
	}

	return nil, nil
}

// Run as goroutiune
func (c *Controller) makeGPTPartition(device *diskv1.BlockDevice) {
	cmdErr := disk.MakeGPTPartition(device.Spec.DevPath)
	device, err := c.blockdeviceCache.Get(device.Namespace, device.Name)
	if err != nil {
		logrus.Error(err)
	}
	deviceCpy := device.DeepCopy()
	if cmdErr == nil {
		// Backwards compatible for LastFormattedAt
		deviceCpy.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
		setDevicePartitioningCondition(deviceCpy, corev1.ConditionFalse, "")
		setDevicePartitionedCondition(deviceCpy, corev1.ConditionTrue, "")
	} else {
		setDevicePartitioningCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
		setDevicePartitionedCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
	}
	if _, err := c.blockdevices.Update(deviceCpy); err != nil {
		logrus.Error(err)
	}
}

func (c *Controller) formatPartition(device *diskv1.BlockDevice) {
	cmdErr := disk.MakeExt4DiskFormatting(device.Spec.DevPath)
	device, err := c.blockdeviceCache.Get(device.Namespace, device.Name)
	if err != nil {
		logrus.Error(err)
		return
	}
	deviceCpy := device.DeepCopy()
	if cmdErr == nil {
		// Backwards compatible for LastFormattedAt
		deviceCpy.Status.DeviceStatus.FileSystem.LastFormattedAt = &metav1.Time{Time: time.Now()}
		setDeviceFormattingCondition(deviceCpy, corev1.ConditionFalse, "")
		setDeviceFormattedCondition(deviceCpy, corev1.ConditionTrue, "")
	} else {
		setDeviceFormattingCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
		setDeviceFormattedCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
	}
	if _, err := c.blockdevices.Update(deviceCpy); err != nil {
		logrus.Error(err)
		return
	}
}

func (c *Controller) updateFilesystemMount(device *diskv1.BlockDevice, fs *block.FileSystemInfo) {
	if fs.MountPoint == "" {
		c.mountFilesystem(device, fs)
	} else {
		c.unmountFilesystem(device, fs)
	}
}

func (c *Controller) unmountFilesystem(device *diskv1.BlockDevice, fs *block.FileSystemInfo) {
	cmdErr := disk.UmountDisk(device.Spec.FileSystem.MountPoint)
	device, err := c.blockdeviceCache.Get(device.Namespace, device.Name)
	if err != nil {
		logrus.Error(err)
		return
	}
	deviceCpy := device.DeepCopy()
	if cmdErr == nil {
		setDeviceUnmountingCondition(deviceCpy, corev1.ConditionFalse, "")
		setDeviceMountedCondition(deviceCpy, corev1.ConditionFalse, "")
	} else {
		setDeviceUnmountingCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
	}
	if _, err := c.blockdevices.Update(deviceCpy); err != nil {
		logrus.Error(err)
		return
	}
}

func (c *Controller) mountFilesystem(device *diskv1.BlockDevice, fs *block.FileSystemInfo) {
	cmdErr := disk.MountDisk(device.Spec.DevPath, device.Spec.FileSystem.MountPoint)
	device, err := c.blockdeviceCache.Get(device.Namespace, device.Name)
	if err != nil {
		logrus.Error(err)
		return
	}
	deviceCpy := device.DeepCopy()
	if cmdErr == nil {
		setDeviceMountingCondition(deviceCpy, corev1.ConditionFalse, "")
		setDeviceMountedCondition(deviceCpy, corev1.ConditionTrue, "")
	} else {
		setDeviceMountingCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
		setDeviceMountedCondition(deviceCpy, corev1.ConditionFalse, cmdErr.Error())
	}
	if _, err := c.blockdevices.Update(deviceCpy); err != nil {
		logrus.Error(err)
		return
	}
}

func (c *Controller) provisionDevice(device *diskv1.BlockDevice, filesystem *block.FileSystemInfo) error {
	node, err := c.getNode()
	if err != nil {
		return err
	}

	mountPoint := device.Spec.FileSystem.MountPoint
	if disk, ok := node.Spec.Disks[device.Name]; ok && disk.Path == mountPoint {
		return nil
	}

	nodeCpy := node.DeepCopy()
	diskSpec := lhtypes.DiskSpec{
		Path:              mountPoint,
		AllowScheduling:   true,
		EvictionRequested: false,
		StorageReserved:   0,
		Tags:              []string{},
	}
	nodeCpy.Spec.Disks[device.Name] = diskSpec
	if _, err = c.nodes.Update(nodeCpy); err != nil {
		return err
	}

	return nil
}

func (c *Controller) unprovisionDevice(device *diskv1.BlockDevice) error {
	updateDeviceErrorCondition := func(device *diskv1.BlockDevice, err error) error {
		setDeviceUnprovisioningCondition(device, corev1.ConditionFalse, err.Error())
		_, err = c.blockdevices.Update(device)
		return err
	}

	updateDeviceCondition := func(device *diskv1.BlockDevice) error {
		setDeviceProvisionedCondition(device, corev1.ConditionFalse, "")
		setDeviceUnprovisioningCondition(device, corev1.ConditionTrue, "")
		_, err := c.blockdevices.Update(device)
		return err
	}

	node, err := c.getNode()
	if err != nil {
		if errors.IsNotFound(err) {
			// Skip since the node is not there.
			return updateDeviceCondition(device)
		}
		return updateDeviceErrorCondition(device, err)
	}

	diskToRemove, ok := node.Spec.Disks[device.Name]
	if !ok {
		logrus.Errorf("disk %s not in disks of longhorn node %s/%s", device.Name, c.namespace, c.nodeName)
		return nil
	}

	isUnprovisioning := false
	for _, tag := range diskToRemove.Tags {
		if tag == util.DiskRemoveTag {
			isUnprovisioning = true
			break
		}
	}

	if isUnprovisioning {
		if status, ok := node.Status.DiskStatus[device.Name]; ok && len(status.ScheduledReplica) == 0 {
			// Unprovision finished. Remove the disk.
			nodeCpy := node.DeepCopy()
			delete(nodeCpy.Spec.Disks, device.Name)
			if _, err := c.nodes.Update(nodeCpy); err != nil {
				return updateDeviceErrorCondition(device, err)
			}
			logrus.Debugf("device %s is unprovisioned", device.Name)
			return updateDeviceCondition(device)
		}
		// Still unprovisioning
		logrus.Debugf("device %s is unprovisioning", device.Name)
		c.blockdevices.EnqueueAfter(device.Namespace, device.Name, enqueueDelay)
		return nil
	}

	// Start unprovisioing
	diskToRemove.AllowScheduling = false
	diskToRemove.EvictionRequested = true
	diskToRemove.Tags = append(diskToRemove.Tags, util.DiskRemoveTag)
	nodeCpy := node.DeepCopy()
	nodeCpy.Spec.Disks[device.Name] = diskToRemove
	if _, err := c.nodes.Update(nodeCpy); err != nil {
		return updateDeviceErrorCondition(device, err)
	}
	return nil
}

// OnBlockDeviceDelete will delete the block devices that belongs to the same parent device
func (c *Controller) OnBlockDeviceDelete(key string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device == nil {
		return nil, nil
	}

	bds, err := c.blockdeviceCache.List(c.namespace, labels.SelectorFromSet(map[string]string{
		corev1.LabelHostname: c.nodeName,
		ParentDeviceLabel:    device.Name,
	}))
	if err != nil {
		return device, err
	}

	if len(bds) == 0 {
		return nil, nil
	}

	// Remove dangling blockdevice partitions
	for _, bd := range bds {
		if err := c.blockdevices.Delete(c.namespace, bd.Name, &metav1.DeleteOptions{}); err != nil {
			return device, err
		}
	}

	// Clean disk from related longhorn node
	node, err := c.getNode()
	if err != nil && !errors.IsNotFound(err) {
		return device, err
	}
	if node == nil {
		logrus.Debugf("node %s is not there. Skip disk deletion from node", c.nodeName)
		return nil, nil
	}
	nodeCpy := node.DeepCopy()
	for _, bd := range bds {
		if _, ok := nodeCpy.Spec.Disks[bd.Name]; !ok {
			logrus.Debugf("disk %s not found in disks of longhorn node %s/%s", bd.Name, c.namespace, c.nodeName)
			continue
		}
		filesystem := c.scanner.BlockInfo.GetFileSystemInfoByDevPath(bd.Spec.DevPath)
		if filesystem.MountPoint != "" {
			if err := disk.UmountDisk(filesystem.MountPoint); err != nil {
				logrus.Warnf("cannot umount disk %s from mount point %s, err: %s", bd.Name, filesystem.MountPoint, err.Error())
			}
		}
		delete(nodeCpy.Spec.Disks, bd.Name)
	}
	if _, err := c.nodes.Update(nodeCpy); err != nil {
		return device, err
	}

	return nil, nil
}

func (c *Controller) getNode() (*longhornv1.Node, error) {
	node, err := c.nodeCache.Get(c.namespace, c.nodeName)
	if err != nil && errors.IsNotFound(err) {
		node, err = c.nodes.Get(c.namespace, c.nodeName, metav1.GetOptions{})
	}
	return node, err
}
