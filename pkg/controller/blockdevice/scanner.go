package blockdevice

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/filter"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/option"
)

const (
	defaultRescanInterval = 30 * time.Second
)

type Scanner struct {
	ctx context.Context

	namespace string
	nodeName  string

	Blockdevices     ctldiskv1.BlockDeviceController
	BlockdeviceCache ctldiskv1.BlockDeviceCache
	BlockInfo        block.Info

	excludeFilters       []*filter.Filter
	autoProvisionFilters []*filter.Filter
	rescanInterval       time.Duration
}

// Scanner scans blockdevices on the node periodically via Scanner.StartScanning.
func NewScanner(
	ctx context.Context,
	bds ctldiskv1.BlockDeviceController,
	block block.Info,
	opt *option.Option,
	excludeFilters []*filter.Filter,
	autoProvisionFilters []*filter.Filter,
) *Scanner {
	// Rescan devices on the node periodically.
	rescanInterval := defaultRescanInterval
	if opt.RescanInterval > 0 {
		rescanInterval = time.Duration(opt.RescanInterval) * time.Second
	}

	return &Scanner{
		ctx:                  ctx,
		namespace:            opt.Namespace,
		nodeName:             opt.NodeName,
		Blockdevices:         bds,
		BlockdeviceCache:     bds.Cache(),
		BlockInfo:            block,
		excludeFilters:       excludeFilters,
		autoProvisionFilters: autoProvisionFilters,
		rescanInterval:       rescanInterval,
	}
}

func (s *Scanner) StartScanning() error {
	if err := s.scanBlockDevicesOnNode(); err != nil {
		return err
	}

	go func() {
		ticker := time.NewTicker(s.rescanInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.scanBlockDevicesOnNode(); err != nil {
					logrus.Errorf("Failed to rescan block devices on node %s: %v", s.nodeName, err)
				}
			case <-s.ctx.Done():
				return
			}
		}
	}()

	return nil
}

// scanBlockDevicesOnNode scans block devices on the node, and it will either create or update them.
func (s *Scanner) scanBlockDevicesOnNode() error {
	logrus.Infof("Scan block devices of node: %s", s.nodeName)
	newBds := make([]*diskv1.BlockDevice, 0)

	autoProvisionedMap := make(map[string]bool, 0)

	// list all the block devices
	for _, disk := range s.BlockInfo.GetDisks() {
		// ignore block device by filters
		if s.ApplyExcludeFiltersForDisk(disk) {
			continue
		}

		logrus.Debugf("Found a disk block device /dev/%s", disk.Name)

		bd := GetDiskBlockDevice(disk, s.nodeName, s.namespace)
		if bd.ObjectMeta.Name == "" {
			logrus.Infof("Skip adding non-identifiable block device %s", bd.Spec.DevPath)
			continue
		}

		if s.ApplyAutoProvisionFiltersForDisk(disk) {
			autoProvisionedMap[bd.Name] = true
		}

		newBds = append(newBds, bd)

		for _, part := range disk.Partitions {
			// ignore block device by filters
			if s.ApplyExcludeFiltersForPartition(part) {
				continue
			}
			logrus.Debugf("Found a partition block device /dev/%s", part.Name)
			bd := GetPartitionBlockDevice(part, s.nodeName, s.namespace)
			if bd.Name == "" {
				logrus.Infof("Skip adding non-identifiable block device %s", bd.Spec.DevPath)
				continue
			}
			newBds = append(newBds, bd)
		}
	}

	oldBdList, err := s.Blockdevices.List(s.namespace, v1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", corev1.LabelHostname, s.nodeName),
	})
	if err != nil {
		return err
	}

	oldBds := convertBlockDeviceListToMap(oldBdList)

	// either create or update the block device
	for _, bd := range newBds {
		bd, err := s.SaveBlockDevice(bd, oldBds, autoProvisionedMap[bd.Name])
		if err != nil {
			return err
		}
		// remove blockdevice from old device so we can delete missing devices afterward
		delete(oldBds, bd.Name)
	}

	// This oldBds are leftover after running SaveBlockDevice.
	// Clean up all previous registered block devices.
	for _, oldBd := range oldBds {
		if err := s.Blockdevices.Delete(oldBd.Namespace, oldBd.Name, &metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// SaveBlockDevice persists the blockedevice information. If oldBds contains a
// blockedevice under the same name (GUID), it will only do an update, otherwise
// create a new one.
//
// Note that this method also activate the device if it's previously inactive.
func (s *Scanner) SaveBlockDevice(
	bd *diskv1.BlockDevice,
	oldBds map[string]*diskv1.BlockDevice,
	autoProvisioned bool,
) (*diskv1.BlockDevice, error) {
	provision := func(bd *diskv1.BlockDevice) {
		logrus.Infof("Block device %s with devPath %s will be auto-provisioned", bd.Name, bd.Spec.DevPath)
		setDeviceAutoProvisionDetectedCondition(bd, corev1.ConditionTrue, "")
		bd.Spec.FileSystem.ForceFormatted = true
		bd.Spec.FileSystem.MountPoint = fmt.Sprintf("/var/lib/harvester/extra-disks/%s", bd.Name)
	}

	if oldBd, ok := oldBds[bd.Name]; ok {
		newStatus := bd.Status.DeviceStatus
		oldStatus := oldBd.Status.DeviceStatus

		// Only disk hasn't yet been formatted/partitioned can be auto-provisioned.
		autoProvisioned = autoProvisioned && diskv1.ProvisionPhaseUnprovisioned.Matches(oldBd)

		if autoProvisioned || !reflect.DeepEqual(oldStatus, newStatus) || oldBd.Status.State != diskv1.BlockDeviceActive {
			logrus.Infof("Update existing block device status %s with devPath: %s", oldBd.Name, oldBd.Spec.DevPath)
			toUpdate := oldBd.DeepCopy()
			toUpdate.Status.State = diskv1.BlockDeviceActive
			toUpdate.Status.DeviceStatus = newStatus
			if autoProvisioned {
				provision(toUpdate)
			}
			return s.Blockdevices.Update(toUpdate)
		}
		return oldBd, nil
	}

	if autoProvisioned {
		provision(bd)
	}
	logrus.Infof("Add new block device %s with device: %s", bd.Name, bd.Spec.DevPath)
	return s.Blockdevices.Create(bd)
}

// ApplyAutoProvisionFiltersForDisk check the status of disk for every
// registered auto-provision filters. If the disk meets one of the criteria, it
// returns true.
func (s *Scanner) ApplyAutoProvisionFiltersForDisk(disk *block.Disk) bool {
	for _, filter := range s.autoProvisionFilters {
		if filter.ApplyDiskFilter(disk) {
			logrus.Debugf("block device /dev/%s is promoted to auto-provision by %s", disk.Name, filter.Name)
			return true
		}
	}
	return false
}

// ApplyExcludeFiltersForPartition check the status of disk for every
// registered exclude filters. If the disk meets one of the criteria, it
// returns true.
func (s *Scanner) ApplyExcludeFiltersForDisk(disk *block.Disk) bool {
	for _, filter := range s.excludeFilters {
		if filter.ApplyDiskFilter(disk) {
			logrus.Debugf("block device /dev/%s ignored by %s", disk.Name, filter.Name)
			return true
		}
	}
	return false
}

// ApplyExcludeFiltersForPartition check the status of partition for every
// registered exclude filters. If the partition meets one of the criteria, it
// returns true.
func (s *Scanner) ApplyExcludeFiltersForPartition(part *block.Partition) bool {
	for _, filter := range s.excludeFilters {
		if filter.ApplyPartFilter(part) {
			logrus.Debugf("block device /dev/%s ignored by %s", part.Name, filter.Name)
			return true
		}
	}
	return false
}

func convertBlockDeviceListToMap(bdList *diskv1.BlockDeviceList) map[string]*diskv1.BlockDevice {
	bds := make([]*diskv1.BlockDevice, 0, len(bdList.Items))
	for _, bd := range bdList.Items {
		bd := bd
		bds = append(bds, &bd)
	}
	return ConvertBlockDevicesToMap(bds)
}

// ConvertBlockDevicesToMap converts a BlockDeviceList to a map with GUID (Name) as keys.
func ConvertBlockDevicesToMap(bds []*diskv1.BlockDevice) map[string]*diskv1.BlockDevice {
	bdMap := make(map[string]*diskv1.BlockDevice, len(bds))
	for _, bd := range bds {
		bdMap[bd.Name] = bd
	}
	return bdMap
}
