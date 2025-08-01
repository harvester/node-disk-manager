package blockdevice

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	ctlharvesterv1 "github.com/harvester/harvester/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/strings/slices"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/filter"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/provisioner"
)

type Scanner struct {
	NodeName             string
	Namespace            string
	UpgradeClient        ctlharvesterv1.UpgradeClient
	Blockdevices         ctldiskv1.BlockDeviceController
	BlockInfo            block.Info
	ExcludeFilters       []*filter.Filter
	AutoProvisionFilters []*filter.Filter
	Cond                 *sync.Cond
	Shutdown             bool
	TerminatedChannels   *chan bool
}

type deviceWithAutoProvision struct {
	bd              *diskv1.BlockDevice
	AutoProvisioned bool
}

func NewScanner(
	nodeName, namespace string,
	upgrades ctlharvesterv1.UpgradeController,
	bds ctldiskv1.BlockDeviceController,
	block block.Info,
	excludeFilters, autoProvisionFilters []*filter.Filter,
	cond *sync.Cond,
	shutdown bool,
	ch *chan bool,
) *Scanner {
	return &Scanner{
		NodeName:             nodeName,
		Namespace:            namespace,
		Blockdevices:         bds,
		UpgradeClient:        upgrades,
		BlockInfo:            block,
		ExcludeFilters:       excludeFilters,
		AutoProvisionFilters: autoProvisionFilters,
		Cond:                 cond,
		Shutdown:             shutdown,
		TerminatedChannels:   ch,
	}
}

func (s *Scanner) Start() error {
	if err := s.scanBlockDevicesOnNode(); err != nil {
		return err
	}
	go func() {
		for {
			s.Cond.L.Lock()
			logrus.Infof("Waiting new event to trigger...")
			s.Cond.Wait()

			if s.Shutdown {
				logrus.Info("Prepare to stop scanner.")
				s.Cond.L.Unlock()
				logrus.Info("Receiver routine shutdown.")
				*s.TerminatedChannels <- true
				return
			}

			logrus.Infof("scanner waked up, do scan...")
			if err := s.scanBlockDevicesOnNode(); err != nil {
				logrus.Errorf("Failed to rescan block devices on node %s: %v", s.NodeName, err)
			}
			s.Cond.L.Unlock()
		}
	}()
	return nil
}

func (s *Scanner) collectAllDevices() []*deviceWithAutoProvision {
	allDevices := make([]*deviceWithAutoProvision, 0)
	// list all the block devices
	for _, disk := range s.BlockInfo.GetDisks() {
		// ignore block device by filters
		if s.ApplyExcludeFiltersForDisk(disk) {
			continue
		}
		logrus.Debugf("Found a disk block device /dev/%s", disk.Name)
		bd := GetDiskBlockDevice(disk, s.NodeName, s.Namespace)
		if bd.Name == "" {
			logrus.Infof("Skip adding non-identifiable block device /dev/%s", disk.Name)
			continue
		}
		logrus.Infof("Detected the disk with block device /dev/%s, id(Name): %s on node %s", disk.Name, bd.Name, s.NodeName)
		logrus.Infof("  - wwn: %v", bd.Status.DeviceStatus.Details.WWN)
		logrus.Infof("  - vendor: %v", bd.Status.DeviceStatus.Details.Vendor)
		logrus.Infof("  - model: %v", bd.Status.DeviceStatus.Details.Model)
		logrus.Infof("  - SerialNumber: %v", bd.Status.DeviceStatus.Details.SerialNumber)
		autoProv := s.ApplyAutoProvisionFiltersForDisk(disk)
		allDevices = append(allDevices, &deviceWithAutoProvision{bd: bd, AutoProvisioned: autoProv})

		for _, part := range disk.Partitions {
			// ignore block device by filters
			if s.ApplyExcludeFiltersForPartition(part) {
				continue
			}
			logrus.Debugf("Found a partition block device /dev/%s", part.Name)
			bd := GetPartitionBlockDevice(part, s.NodeName, s.Namespace)
			if bd.Name == "" {
				logrus.Infof("Skip adding non-identifiable block device %s", bd.Spec.DevPath)
				continue
			}
			allDevices = append(allDevices, &deviceWithAutoProvision{bd: bd, AutoProvisioned: false})
		}
	}
	return allDevices
}

func (s *Scanner) handleExistingDev(oldBd *diskv1.BlockDevice, newBd *diskv1.BlockDevice, autoProvisioned bool) {
	if isDevPathChanged(oldBd, newBd) {
		// Dev Path will change when device is temporarily gone and back.
		// Node reboot might also cause the device path change (depends on device interrupt).
		// According to the above, we should update the device path once it changes.
		if oldBd.Status.State == diskv1.BlockDeviceInactive {
			logrus.Infof("The inactive block device %s with wwn %s is coming back", newBd.Name, newBd.Status.DeviceStatus.Details.WWN)
			oldBd.Status.State = diskv1.BlockDeviceActive
		}
		logrus.Infof("Try to update the device path of %s, which is changed from %s to %s.", oldBd.Name, oldBd.Status.DeviceStatus.DevPath, newBd.Status.DeviceStatus.DevPath)
		oldBd.Status.DeviceStatus.DevPath = newBd.Status.DeviceStatus.DevPath
		if _, err := s.Blockdevices.Update(oldBd); err != nil {
			logrus.Errorf("Update device %s status error, wake up scanner again: %v", oldBd.Name, err)
			s.Cond.Signal()
		}
	} else if isDevAlreadyProvisioned(newBd) {
		logrus.Debugf("Skip the provisioned device: %s", newBd.Name)
	} else if s.NeedsAutoProvision(oldBd, autoProvisioned) {
		logrus.Debugf("Enqueue block device %s for auto-provisioning", newBd.Name)
		s.Blockdevices.Enqueue(s.Namespace, newBd.Name)
	} else {
		logrus.Debugf("Skip updating device %s", newBd.Name)
	}
}

func (s *Scanner) deactivateBlockDevices(oldBds map[string]*diskv1.BlockDevice) error {
	for _, oldBd := range oldBds {
		if oldBd.Status.State == diskv1.BlockDeviceInactive {
			logrus.Debugf("The device %s is already inactive, continue.", oldBd.Name)
			continue
		}
		logrus.Debugf("Change the device %s to inactive.", oldBd.Name)
		newBd := oldBd.DeepCopy()
		newBd.Status.State = diskv1.BlockDeviceInactive
		if !reflect.DeepEqual(oldBd, newBd) {
			logrus.Debugf("Update block device %s for new formatting and mount state", oldBd.Name)
			if _, err := s.Blockdevices.Update(newBd); err != nil {
				logrus.Errorf("Update device %s status error", oldBd.Name)
				return err
			}
		}
	}
	return nil
}

// scanBlockDevicesOnNode scans block devices on the node, and it will either create or update them.
func (s *Scanner) scanBlockDevicesOnNode() error {
	logrus.Debugf("Scan block devices of node: %s", s.NodeName)

	// list all the block devices
	allDevices := s.collectAllDevices()

	oldBdList, err := s.Blockdevices.List(s.Namespace, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", corev1.LabelHostname, s.NodeName),
	})
	if err != nil {
		return err
	}

	oldBds, existingWWNs := convertBlockDeviceListToMap(oldBdList)
	logrus.Debugf("The current BdList: %+v", oldBds)
	for _, device := range allDevices {
		bd := device.bd
		autoProvisioned := device.AutoProvisioned
		logrus.Debugf("Processing block device %s with wwn: %s", bd.Name, bd.Status.DeviceStatus.Details.WWN)
		if oldBd, ok := oldBds[bd.Name]; ok {
			s.handleExistingDev(oldBd, bd, autoProvisioned)
			// only first time to update the cache
			if !CacheDiskTags.Initialized() && oldBd.Spec.Tags != nil && len(oldBd.Spec.Tags) > 0 {
				CacheDiskTags.UpdateDiskTags(oldBd.Name, oldBd.Spec.Tags)
			}
			// remove blockdevice from old device so we can delete missing devices afterward
			delete(oldBds, bd.Name)
		} else {
			if bd.Status.DeviceStatus.Details.WWN != "unknown" {
				/*
				* Prevent add duplicated wwn even if the device path is different.
				* That prevent the device from being formatted again.
				* We should use multiple device path (TBD) for the same wwn.
				 */
				if slices.Contains(existingWWNs, bd.Status.DeviceStatus.Details.WWN) {
					logrus.Warnf("Skip adding duplicated WWN device %s, device path: %s", bd.Status.DeviceStatus.Details.WWN, bd.Spec.DevPath)
					continue
				}
				existingWWNs = append(existingWWNs, bd.Status.DeviceStatus.Details.WWN)
				logrus.Debugf("The current WWNs are: %v", existingWWNs)
			} else {
				// If the WWN is "unknown" it means the disk is identified by FS UUID, so skip the duplicate check
				logrus.WithFields(logrus.Fields{
					"name":   bd.Name,
					"device": bd.Spec.DevPath,
				}).Info("Skipping duplicate disk check (device has no WWN)")
			}
			logrus.Infof("Create new device %s with wwn: %s", bd.Name, bd.Status.DeviceStatus.Details.WWN)
			if _, err := s.SaveBlockDevice(bd, autoProvisioned); err != nil && !errors.IsAlreadyExists(err) {
				return err
			}
		}
	}
	if !CacheDiskTags.Initialized() {
		CacheDiskTags.UpdateInitialized()
		logrus.Debugf("CacheDiskTags initialized: %+v", CacheDiskTags)
	}

	// We do not remove the block device that maybe just temporily not available.
	// Set it to inactive and give the chance to recover.
	if err := s.deactivateBlockDevices(oldBds); err != nil {
		return err
	}
	return nil
}

func convertBlockDeviceListToMap(bdList *diskv1.BlockDeviceList) (map[string]*diskv1.BlockDevice, []string) {
	var wwns = make([]string, len(bdList.Items))
	bdMap := make(map[string]*diskv1.BlockDevice, len(bdList.Items))
	for order, bd := range bdList.Items {
		bd := bd
		bdMap[bd.Name] = &bd
		wwns[order] = bd.Status.DeviceStatus.Details.WWN
	}
	return bdMap, wwns
}

// ApplyExcludeFiltersForPartition check the status of disk for every
// registered exclude filters. If the disk meets one of the criteria, it
// returns true.
func (s *Scanner) ApplyExcludeFiltersForDisk(disk *block.Disk) bool {
	for _, filter := range s.ExcludeFilters {
		if strings.HasPrefix(disk.Name, "dm-") {
			// None of the existing filters can handle this case, but
			// we need to exclude /dev/dm-* devices because they appear
			// when LHv2 volumes are attached to VMs.
			logrus.Debugf("block device /dev/%s ignored because it's a dm device", disk.Name)
			return true
		}
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
	for _, filter := range s.ExcludeFilters {
		if filter.ApplyPartFilter(part) {
			logrus.Debugf("block device /dev/%s ignored by %s", part.Name, filter.Name)
			return true
		}
	}
	return false
}

// ApplyAutoProvisionFiltersForDisk check the status of disk for every
// registered auto-provision filters. If the disk meets one of the criteria, it
// returns true.
func (s *Scanner) ApplyAutoProvisionFiltersForDisk(disk *block.Disk) bool {
	for _, filter := range s.AutoProvisionFilters {
		if filter.ApplyDiskFilter(disk) {
			logrus.Debugf("block device /dev/%s is promoted to auto-provision by %s", disk.Name, filter.Name)
			return true
		}
	}
	return false
}

// SaveBlockDevice persists the blockedevice information.
func (s *Scanner) SaveBlockDevice(bd *diskv1.BlockDevice, autoProvisioned bool) (*diskv1.BlockDevice, error) {
	_, err := s.Blockdevices.Get(bd.Namespace, bd.Name, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			if autoProvisioned && canAutoProvision(s.UpgradeClient) {
				bd.Spec.FileSystem.ForceFormatted = true
				bd.Spec.Provision = true
				bd.Spec.Provisioner = &diskv1.ProvisionerInfo{
					Longhorn: &diskv1.LonghornProvisionerInfo{
						EngineVersion: provisioner.TypeLonghornV1,
					},
				}
			}
			logrus.Infof("Add new block device %s with device: %s", bd.Name, bd.Spec.DevPath)
			return s.Blockdevices.Create(bd)
		}
		return nil, err
	}
	logrus.Warnf("Should be handled by existing device, coming bd: %v", bd)
	return nil, nil
}

// NeedsAutoProvision returns true if the current block device needs to be auto-provisioned.
//
// Criteria:
// - disk hasn't yet set to provisioned
// - disk hasn't yet been force formatted
// - disk matches auto-provisioned patterns
func (s *Scanner) NeedsAutoProvision(oldBd *diskv1.BlockDevice, autoProvisionPatternMatches bool) bool {
	return !oldBd.Spec.Provision && autoProvisionPatternMatches && oldBd.Status.DeviceStatus.FileSystem.LastFormattedAt == nil
}

// isDevPathChanged returns true if the device path has changed.
//
// The device path changed on when the device is temporarily gone and back.
// We init the device status when the first time the device is found.
// If the device is active, the device path should not change.
func isDevPathChanged(oldBd *diskv1.BlockDevice, newBd *diskv1.BlockDevice) bool {
	return oldBd.Status.DeviceStatus.DevPath != newBd.Status.DeviceStatus.DevPath
}

/* isDevAlreadyProvisioned would return true if the device is provisioned */
func isDevAlreadyProvisioned(newBd *diskv1.BlockDevice) bool {
	return newBd.Status.ProvisionPhase == diskv1.ProvisionPhaseProvisioned
}
