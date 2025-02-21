package blockdevice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	gocommon "github.com/harvester/go-common"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta2"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/provisioner"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

const (
	blockDeviceHandlerName = "harvester-block-device-handler"
)

type Controller struct {
	Namespace string
	NodeName  string

	NodeCache ctllonghornv1.NodeCache
	Nodes     ctllonghornv1.NodeClient

	Blockdevices     ctldiskv1.BlockDeviceController
	BlockdeviceCache ctldiskv1.BlockDeviceCache
	BlockInfo        block.Info

	LVMVgClient     ctldiskv1.LVMVolumeGroupController
	provisionerLock *sync.Mutex // Lock for some specific provisioner operations, e.g. LVM

	scanner   *Scanner
	semaphore *provisioner.Semaphore
}

type NeedMountUpdateOP int8

const (
	NeedMountUpdateNoOp NeedMountUpdateOP = 1 << iota
	NeedMountUpdateMount
	NeedMountUpdateUnmount
)

func (f NeedMountUpdateOP) Has(flag NeedMountUpdateOP) bool {
	return f&flag != 0
}

var CacheDiskTags *provisioner.DiskTags

// Register register the block device CRD controller
func Register(
	ctx context.Context,
	nodes ctllonghornv1.NodeController,
	bds ctldiskv1.BlockDeviceController,
	lvmVGs ctldiskv1.LVMVolumeGroupController,
	block block.Info,
	opt *option.Option,
	scanner *Scanner,
) error {
	CacheDiskTags = provisioner.NewLonghornDiskTags()
	semaphoreObj := provisioner.NewSemaphore(opt.MaxConcurrentOps)
	controller := &Controller{
		Namespace:        opt.Namespace,
		NodeName:         opt.NodeName,
		NodeCache:        nodes.Cache(),
		Nodes:            nodes,
		Blockdevices:     bds,
		BlockdeviceCache: bds.Cache(),
		LVMVgClient:      lvmVGs,
		BlockInfo:        block,
		scanner:          scanner,
		semaphore:        semaphoreObj,
		provisionerLock:  &sync.Mutex{},
	}

	if err := scanner.Start(); err != nil {
		return err
	}

	utils.CallerWithCondLock(scanner.Cond, func() any {
		logrus.Infof("Wake up scanner first time to update CacheDiskTags ...")
		scanner.Cond.Signal()
		return nil
	})

	bds.OnChange(ctx, blockDeviceHandlerName, controller.OnBlockDeviceChange)
	bds.OnRemove(ctx, blockDeviceHandlerName, controller.OnBlockDeviceDelete)
	return nil
}

// OnBlockDeviceChange watch the block device CR on change and performing disk operations
// like mounting the disks to a desired path via ext4
func (c *Controller) OnBlockDeviceChange(_ string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if canSkipBlockDeviceChange(device, c.NodeName) {
		return nil, nil
	}

	// give another chance to update provision for auto provision device
	if len(c.scanner.AutoProvisionFilters) > 0 && !device.Spec.Provision && device.Status.DeviceStatus.FileSystem.LastFormattedAt == nil {
		if devNew, needUpdated := c.updateAutoProvisionDevice(device); needUpdated {
			return c.Blockdevices.Update(devNew)
		}
	}

	deviceCpy := device.DeepCopy()
	provisionerInst, err := c.generateProvisioner(deviceCpy)
	if err != nil {
		logrus.Warnf("Failed to generate provisioner for device %s: %v", device.Name, err)
		return nil, err
	}
	if !reflect.DeepEqual(device, deviceCpy) {
		logrus.Debugf("Update block device %s for new provisioner state", device.Name)
		return c.Blockdevices.Update(deviceCpy)
	}
	if provisionerInst == nil {
		logrus.Infof("Skip device %s as no provisioner found or not configured", device.Name)
		return nil, nil
	}

	// handle remove device no matter inactive or corrupted, we will set `device.Spec.FileSystem.Provisioned` to false
	if needProvisionerUnprovision(device) {
		requeue, err := provisionerInst.UnProvision()
		c.handleCondDiskAddedToNodeAndRequeue(deviceCpy, err, requeue)

		if !reflect.DeepEqual(device, deviceCpy) {
			logrus.Debugf("Update block device %s after removing", device.Name)
			return c.Blockdevices.Update(deviceCpy)
		}
	}

	// corrupted device could be skipped if we do not set ForceFormatted or Repaired
	if deviceIsNotActiveOrCorrupted(device) {
		logrus.Infof("Skip inactive or corrupted device %s", device.Name)
		return nil, nil
	}

	devPath, err := provisioner.ResolvePersistentDevPath(device)
	if err != nil {
		return nil, err
	}

	logrus.Debugf("Checking to format device %s", device.Name)
	if formatted, requeue, err := provisionerInst.Format(devPath); !formatted {
		if requeue {
			c.Blockdevices.EnqueueAfter(c.Namespace, device.Name, jitterEnqueueDelay())
		}
		if !reflect.DeepEqual(device, deviceCpy) {
			logrus.Debugf("Update block device %s for new formatting state", device.Name)
			return c.Blockdevices.Update(deviceCpy)
		}

		return device, err
	}

	/*
	 * Spec.Filesystem.Provisioned: What we desired to do
	 * Status.ProvisionPhase: What we are now
	 * 1. Spec.Filesystem.Provisioned = true, Status.ProvisionPhase = ProvisionPhaseProvisioned
	 *   -> Already provisioned, do Update()
	 * 2. Spec.Filesystem.Provisioned = true, Status.ProvisionPhase = ProvisionPhaseUnprovisioned
	 *   -> Provision the device
	 */
	logrus.Debugf("Checking to provision/update device %s", device.Name)
	if needProvisionerUpdate(device, deviceCpy) {
		logrus.Infof("Prepare to check the new device tags %v with device: %s", deviceCpy.Spec.Tags, device.Name)
		requeue, err := provisionerInst.Update()
		c.handleCondDiskAddedToNodeAndRequeue(deviceCpy, err, requeue)
	}

	if needProvisionerProvision(device, deviceCpy) {
		logrus.Infof("Prepare to provision device %s to node %s", device.Name, c.NodeName)
		requeue, err := provisionerInst.Provision()
		c.handleCondDiskAddedToNodeAndRequeue(deviceCpy, err, requeue)
	}

	return c.finalizeBlockDevice(device, deviceCpy, devPath)
}

func (c *Controller) handleCondDiskAddedToNodeAndRequeue(device *diskv1.BlockDevice, err error, requeue bool) {
	if err != nil {
		diskv1.DiskAddedToNode.SetError(device, "", err)
		diskv1.DiskAddedToNode.SetStatusBool(device, false)
	}
	if requeue {
		c.Blockdevices.EnqueueAfter(c.Namespace, device.Name, jitterEnqueueDelay())
	}
}

func (c *Controller) finalizeBlockDevice(oldBd, newBd *diskv1.BlockDevice, devPath string) (*diskv1.BlockDevice, error) {
	if !reflect.DeepEqual(oldBd, newBd) {
		logrus.Debugf("Update block device %s for new provision state", oldBd.Name)
		return c.Blockdevices.Update(newBd)
	}

	// None of the above operations have resulted in an update to the device.
	// We therefore try to update the latest device status from the OS
	if err := c.updateDeviceStatus(newBd, devPath); err != nil {
		return nil, err
	}

	if !reflect.DeepEqual(oldBd, newBd) {
		logrus.Debugf("Update block device %s for new device status", oldBd.Name)
		return c.Blockdevices.Update(newBd)
	}

	return nil, nil

}

func (c *Controller) generateProvisioner(device *diskv1.BlockDevice) (provisioner.Provisioner, error) {
	// skip the non-provisioned device
	if device.Spec.Provisioner == nil && device.Status.ProvisionPhase != diskv1.ProvisionPhaseProvisioned {
		return nil, nil
	}
	// upgrade case, we need to update some fields
	if device.Spec.Provisioner == nil && device.Status.ProvisionPhase == diskv1.ProvisionPhaseProvisioned {
		device.Spec.Provision = true
		provisionerLHV1 := &diskv1.LonghornProvisionerInfo{
			EngineVersion: provisioner.TypeLonghornV1,
		}
		device.Spec.Provisioner = &diskv1.ProvisionerInfo{
			Longhorn: provisionerLHV1,
		}
		return nil, nil
	}
	logrus.Infof("Generate provisioner from device %s, content: %v", device.Name, device.Spec.Provisioner)
	// set default
	provisionerType := provisioner.TypeLonghornV1
	if device.Spec.Provisioner != nil {
		if device.Spec.Provisioner.Longhorn != nil {
			provisionerType = device.Spec.Provisioner.Longhorn.EngineVersion
		}
		if device.Spec.Provisioner.LVM != nil {
			provisionerType = provisioner.TypeLVM
		}
	}
	switch provisionerType {
	case provisioner.TypeLonghornV1:
		provisionerInfo := &diskv1.LonghornProvisionerInfo{
			EngineVersion: provisionerType,
		}
		provisioner := &diskv1.ProvisionerInfo{
			Longhorn: provisionerInfo,
		}
		device.Spec.Provisioner = provisioner
		return c.generateLHv1Provisioner(device)
	case provisioner.TypeLonghornV2:
		return c.generateLHv2Provisioner(device)
	case provisioner.TypeLVM:
		return c.generateLVMProvisioner(device)
	default:
		return nil, fmt.Errorf("unsupported provisioner type %s", provisionerType)
	}
}

func (c *Controller) generateLHv1Provisioner(device *diskv1.BlockDevice) (provisioner.Provisioner, error) {
	node, err := c.NodeCache.Get(c.Namespace, c.NodeName)
	if apierrors.IsNotFound(err) {
		node, err = c.Nodes.Get(c.Namespace, c.NodeName, metav1.GetOptions{})
	}
	if err != nil {
		return nil, err
	}
	return provisioner.NewLHV1Provisioner(device, c.BlockInfo, node, c.Nodes, c.NodeCache, CacheDiskTags, c.semaphore)
}

func (c *Controller) generateLVMProvisioner(device *diskv1.BlockDevice) (provisioner.Provisioner, error) {
	vgName := device.Spec.Provisioner.LVM.VgName
	return provisioner.NewLVMProvisioner(vgName, c.NodeName, c.LVMVgClient, device, c.BlockInfo, c.provisionerLock)
}

func (c *Controller) generateLHv2Provisioner(device *diskv1.BlockDevice) (provisioner.Provisioner, error) {
	node, err := c.NodeCache.Get(c.Namespace, c.NodeName)
	if apierrors.IsNotFound(err) {
		node, err = c.Nodes.Get(c.Namespace, c.NodeName, metav1.GetOptions{})
	}
	if err != nil {
		return nil, err
	}
	return provisioner.NewLHV2Provisioner(device, c.BlockInfo, node, c.Nodes, c.NodeCache, CacheDiskTags)
}

func (c *Controller) updateDeviceStatus(device *diskv1.BlockDevice, devPath string) error {
	var newStatus diskv1.DeviceStatus
	var needAutoProvision bool

	switch device.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
		disk := c.BlockInfo.GetDiskByDevPath(devPath)
		bd := GetDiskBlockDevice(disk, c.NodeName, c.Namespace)
		newStatus = bd.Status.DeviceStatus
		autoProvisioned := c.scanner.ApplyAutoProvisionFiltersForDisk(disk)
		// Only disk can be auto-provisioned.
		needAutoProvision = c.scanner.NeedsAutoProvision(device, autoProvisioned)
	case diskv1.DeviceTypePart:
		parentDevPath, err := block.GetParentDevName(devPath)
		if err != nil {
			return fmt.Errorf("failed to get parent devPath for %s: %v", device.Name, err)
		}
		part := c.BlockInfo.GetPartitionByDevPath(parentDevPath, devPath)
		bd := GetPartitionBlockDevice(part, c.NodeName, c.Namespace)
		newStatus = bd.Status.DeviceStatus
	default:
		return fmt.Errorf("unknown device type %s", device.Status.DeviceStatus.Details.DeviceType)
	}

	oldStatus := device.Status.DeviceStatus
	lastFormatted := oldStatus.FileSystem.LastFormattedAt
	if lastFormatted != nil && newStatus.FileSystem.LastFormattedAt == nil {
		newStatus.FileSystem.LastFormattedAt = lastFormatted
	}

	// Update device path
	newStatus.DevPath = devPath

	if !reflect.DeepEqual(oldStatus, newStatus) {
		logrus.Infof("Update existing block device status %s", device.Name)
		device.Status.DeviceStatus = newStatus
	}
	// Only disk hasn't yet been formatted can be auto-provisioned.
	if needAutoProvision {
		logrus.Infof("Auto provisioning block device %s", device.Name)
		device.Spec.FileSystem.ForceFormatted = true
		device.Spec.Provision = true
		device.Spec.Provisioner = &diskv1.ProvisionerInfo{
			Longhorn: &diskv1.LonghornProvisionerInfo{
				EngineVersion: provisioner.TypeLonghornV1,
			},
		}

	}
	return nil
}

// OnBlockDeviceDelete will delete the block devices that belongs to the same parent device
func (c *Controller) OnBlockDeviceDelete(_ string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {

	if !CacheDiskTags.Initialized() {
		return nil, errors.New(provisioner.ErrorCacheDiskTagsNotInitialized)
	}

	if device == nil {
		return nil, nil
	}

	bds, err := c.BlockdeviceCache.List(c.Namespace, labels.SelectorFromSet(map[string]string{
		corev1.LabelHostname: c.NodeName,
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
		if err := c.Blockdevices.Delete(c.Namespace, bd.Name, &metav1.DeleteOptions{}); err != nil {
			return device, err
		}
	}

	// Clean disk from related longhorn node
	node, err := c.Nodes.Get(c.Namespace, c.NodeName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return device, err
	}
	if node == nil {
		logrus.Debugf("node %s is not there. Skip disk deletion from node", c.NodeName)
		return nil, nil
	}
	nodeCpy := node.DeepCopy()
	for _, bd := range bds {
		if _, ok := nodeCpy.Spec.Disks[bd.Name]; !ok {
			logrus.Debugf("disk %s not found in disks of longhorn node %s/%s", bd.Name, c.Namespace, c.NodeName)
			continue
		}
		existingMount := bd.Status.DeviceStatus.FileSystem.MountPoint
		if existingMount != "" {
			if err := utils.UmountDisk(existingMount); err != nil {
				logrus.Warnf("cannot umount disk %s from mount point %s, err: %s", bd.Name, existingMount, err.Error())
			}
		}
		delete(nodeCpy.Spec.Disks, bd.Name)
	}
	if _, err := c.Nodes.Update(nodeCpy); err != nil {
		return device, err
	}

	CacheDiskTags.DeleteDiskTags(device.Name)

	return nil, nil
}

func (c *Controller) updateAutoProvisionDevice(device *diskv1.BlockDevice) (*diskv1.BlockDevice, bool) {
	tmpDisk := &block.Disk{
		// we only need the dev path for checking auto provision
		Name: strings.TrimPrefix(device.Status.DeviceStatus.DevPath, "/dev/"),
	}
	if c.scanner.ApplyAutoProvisionFiltersForDisk(tmpDisk) {
		logrus.Debugf("Update auto provision device %s", device.Name)
		deviceCpy := device.DeepCopy()
		deviceCpy.Spec.FileSystem.ForceFormatted = true
		deviceCpy.Spec.Provision = true
		deviceCpy.Spec.Provisioner = &diskv1.ProvisionerInfo{
			Longhorn: &diskv1.LonghornProvisionerInfo{
				EngineVersion: provisioner.TypeLonghornV1,
			},
		}
		return deviceCpy, true
	}
	return nil, false
}

// jitterEnqueueDelay returns a random duration between 3 to 7.
func jitterEnqueueDelay() time.Duration {
	enqueueDelay := 5
	randNum, err := gocommon.GenRandNumber(2)
	if err != nil {
		logrus.Errorf("Failed to generate random number, set randnumber to `0`: %v", err)
	}
	return time.Duration(int(randNum)+enqueueDelay) * time.Second
}

func deviceIsNotActiveOrCorrupted(device *diskv1.BlockDevice) bool {
	return device.Status.State == diskv1.BlockDeviceInactive ||
		(device.Status.DeviceStatus.FileSystem.Corrupted && !device.Spec.FileSystem.ForceFormatted && !device.Spec.FileSystem.Repaired)
}

func canSkipBlockDeviceChange(device *diskv1.BlockDevice, nodeName string) bool {
	return device == nil || device.DeletionTimestamp != nil || device.Spec.NodeName != nodeName
}

func needProvisionerUnprovision(device *diskv1.BlockDevice) bool {
	return !device.Spec.Provision && device.Status.ProvisionPhase != diskv1.ProvisionPhaseUnprovisioned
}

func needProvisionerUpdate(oldBd, newBd *diskv1.BlockDevice) bool {
	return oldBd.Status.ProvisionPhase == diskv1.ProvisionPhaseProvisioned && newBd.Spec.Provision
}

func needProvisionerProvision(oldBd, newBd *diskv1.BlockDevice) bool {
	return oldBd.Status.ProvisionPhase == diskv1.ProvisionPhaseUnprovisioned && newBd.Spec.Provision
}
