package blockdevice

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/google/uuid"
	ctlharvesterv1 "github.com/harvester/harvester/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/jaypipes/ghw/pkg/util"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/filter"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/provisioner"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type Scanner struct {
	NodeName             string
	Namespace            string
	UpgradeClient        ctlharvesterv1.UpgradeClient
	Blockdevices         ctldiskv1.BlockDeviceController
	BlockInfo            block.Info
	ExcludeFilters       []*filter.Filter
	AutoProvisionFilters []*filter.Filter
	ConfigMapLoader      *filter.ConfigMapLoader
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
	configMapLoader *filter.ConfigMapLoader,
	cond *sync.Cond,
	shutdown bool,
	ch *chan bool,
) *Scanner {
	return &Scanner{
		NodeName:           nodeName,
		Namespace:          namespace,
		Blockdevices:       bds,
		UpgradeClient:      upgrades,
		BlockInfo:          block,
		ConfigMapLoader:    configMapLoader,
		Cond:               cond,
		Shutdown:           shutdown,
		TerminatedChannels: ch,
	}
}

func (s *Scanner) Start(ctx context.Context) error {
	// Always scan once on start
	if err := s.scanBlockDevicesOnNode(ctx); err != nil {
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

			logrus.Infof("Scanner woke up, do scan...")
			if err := s.scanBlockDevicesOnNode(ctx); err != nil {
				logrus.Errorf("Failed to rescan block devices on node %s: %v", s.NodeName, err)
			}
			s.Cond.L.Unlock()
		}
	}()
	return nil
}

// collectAllDevices returns a slice containing every BlockDevice on the system.
// The BlockDevices in the list will not have valid names, but the DeviceStatus
// fields (UUID, WWN, Vendor, Model, SerialNumber, BusPath) will have been filled
// in as completely as possible.
func (s *Scanner) collectAllDevices() []*deviceWithAutoProvision {
	allDevices := make([]*deviceWithAutoProvision, 0)
	// list all the block devices
	for _, disk := range s.BlockInfo.GetDisks() {
		logrus.WithFields(logrus.Fields{
			"device": fmt.Sprintf("/dev/%s", disk.Name),
		}).Info("Scanning device")
		// ignore block device by filters
		if s.ApplyExcludeFiltersForDisk(disk) {
			continue
		}
		bd := GetDiskBlockDevice(disk, s.NodeName, s.Namespace)
		logrus.WithFields(logrus.Fields{
			"device":  fmt.Sprintf("/dev/%s", disk.Name),
			"uuid":    bd.Status.DeviceStatus.Details.UUID,         // Can be empty
			"wwn":     bd.Status.DeviceStatus.Details.WWN,          // Can be util.UNKNOWN
			"vendor":  bd.Status.DeviceStatus.Details.Vendor,       // Can be util.UNKNOWN
			"model":   bd.Status.DeviceStatus.Details.Model,        // Can be util.UNKNOWN
			"serial":  bd.Status.DeviceStatus.Details.SerialNumber, // Can be util.UNKNOWN
			"buspath": bd.Status.DeviceStatus.Details.BusPath,      // Can be util.UNKNOWN
		}).Info("Detected disk")
		autoProv := s.ApplyAutoProvisionFiltersForDisk(disk)
		allDevices = append(allDevices, &deviceWithAutoProvision{bd: bd, AutoProvisioned: autoProv})
	}
	return allDevices
}

// handleExistingDev will update an existing BD CR based on the current state
// of an actual block device on the system.  It will return true if newBd
// (the detected block device) really does correspond to oldBd (the BD CR),
// and was potentially updated in some way, or false if newBd doesn't
// actually correspond to oldBd.
//
// Note:
//   - If the device was inactive before it's OK for its status to change,
//     notably the device path.  It should also be fine if the buspath
//     changes (maybe it got moved somwehere else)
//   - Changes of vendor+model+serial would always be unexpected
//   - Changes of UUID might be unexpected (unless a UUID got added due
//     to a device being provisioned, but that shouldn't come by this
//     code path).
//   - Changes of WWN might be unexepcted, except in that weird case where
//     a kernel update changed the WWNs of existing disks.
func (s *Scanner) handleExistingDev(oldBd *diskv1.BlockDevice, newBd *diskv1.BlockDevice, autoProvisioned bool) bool {
	oldBdCp := oldBd.DeepCopy()

	if oldBd.Status.State == diskv1.BlockDeviceActive {
		// The BD is currently active. In this case the dev path shouldn't change, but
		// it's possible that some other details may have changed (the UUID if someone
		// manually created a filesystem on an unprovisioned device, or the WWN in
		// rare cases where a kernel update messes with device naming)
		if isDevPathChanged(oldBd, newBd) {
			// The dev path for an active device has changed.  This means that newBd
			// might not actually correspond to oldBd.  This shouldn't usually happen
			// in normal operation, but can happen if e.g. a multipath BD exists, but
			// multipathd isn't started anymore.  In this case newBd will point to
			// one of the raw devices.  The same is true in reverse.  It can also
			// happen if someone powers down the host and moves a disk from one
			// place to another.  In all these cases we want to skip the update and
			// return false so that later the BD can be marked inactive or removed.
			// For devices that end up with a new path, these will be picked up and
			// re-activated in a subsequent scanner run.
			logrus.WithFields(logrus.Fields{
				"name":      oldBd.Name,
				"device":    oldBd.Status.DeviceStatus.DevPath,
				"newDevice": newBd.Status.DeviceStatus.DevPath,
			}).Warn("new device path detected for active device - skipping update")
			return false
		}
		// DevPath isn't changed, but other things might, e.g. UUID if someone manually formatted a disk
		oldBdCp.Status.DeviceStatus.Capacity = newBd.Status.DeviceStatus.Capacity
		oldBdCp.Status.DeviceStatus.Details = newBd.Status.DeviceStatus.Details
		oldBdCp.Status.DeviceStatus.Partitioned = newBd.Status.DeviceStatus.Partitioned
	} else {
		// The BD is inactive.  This can happen if a provisioned device is
		// temporarily gone and has come back.  It can also happen for provisioned
		// multipath devices if the system is rebooted without multipathd enabled,
		// and then later multipathd is started again.
		if strings.HasPrefix(oldBd.Status.DeviceStatus.DevPath, "/dev/mapper") {
			if isDevPathChanged(oldBd, newBd) {
				logrus.WithFields(logrus.Fields{
					"name":      oldBd.Name,
					"device":    oldBd.Status.DeviceStatus.DevPath,
					"newDevice": newBd.Status.DeviceStatus.DevPath,
				}).Warn("new device path detected for inactive multipath device - skipping update")
				return false
			}
			path, _ := filepath.EvalSymlinks(oldBd.Status.DeviceStatus.DevPath)
			if _, err := utils.IsMultipathDevice(path); err == nil {
				logrus.WithFields(logrus.Fields{
					"name": oldBd.Name,
				}).Info("reactivating multipath device")
				oldBdCp.Status.State = diskv1.BlockDeviceActive
				// DeviceStatus really shouldn't have changed for MP devices, but pick it up anyway just in case
				oldBdCp.Status.DeviceStatus.Capacity = newBd.Status.DeviceStatus.Capacity
				oldBdCp.Status.DeviceStatus.Details = newBd.Status.DeviceStatus.Details
				oldBdCp.Status.DeviceStatus.Partitioned = newBd.Status.DeviceStatus.Partitioned
			}
		} else {
			if isDevPathChanged(oldBd, newBd) {
				logrus.WithFields(logrus.Fields{
					"name":      oldBd.Name,
					"device":    oldBd.Status.DeviceStatus.DevPath,
					"newDevice": newBd.Status.DeviceStatus.DevPath,
				}).Info("reactivating block device with new path")
				oldBdCp.Status.DeviceStatus.DevPath = newBd.Status.DeviceStatus.DevPath
			} else {
				logrus.WithFields(logrus.Fields{
					"name": oldBd.Name,
				}).Infof("reactivating block device")
			}
			oldBdCp.Status.State = diskv1.BlockDeviceActive
			// This pulls in all other possible updates -- wwn, uuid, vendor, model, serial, ...
			oldBdCp.Status.DeviceStatus.Capacity = newBd.Status.DeviceStatus.Capacity
			oldBdCp.Status.DeviceStatus.Details = newBd.Status.DeviceStatus.Details
			oldBdCp.Status.DeviceStatus.Partitioned = newBd.Status.DeviceStatus.Partitioned
		}
	}

	if !reflect.DeepEqual(oldBd, oldBdCp) {
		logrus.WithFields(logrus.Fields{
			"name":      oldBd.Name,
			"status":    fmt.Sprintf("%+v", oldBd.Status.DeviceStatus),
			"newStatus": fmt.Sprintf("%+v", oldBdCp.Status.DeviceStatus),
		}).Info("updating device")
		if _, err := s.Blockdevices.Update(oldBdCp); err != nil {
			logrus.WithFields(logrus.Fields{
				"name": oldBd.Name,
				"err":  err,
			}).Error("error updating device, waking scanner")
			s.Cond.Signal()
		}
	} else if isDevAlreadyProvisioned(oldBd) {
		logrus.WithFields(logrus.Fields{
			"name": oldBd.Name,
		}).Debug("skipping provisioned device")
	} else if s.NeedsAutoProvision(oldBd, autoProvisioned) {
		logrus.WithFields(logrus.Fields{
			"name": oldBd.Name,
		}).Debug("enquing device for auto-provisioning")
		s.Blockdevices.Enqueue(s.Namespace, oldBd.Name)
	} else {
		logrus.WithFields(logrus.Fields{
			"name": oldBd.Name,
		}).Debug("device is unchanged (no need to update)")
	}
	return true
}

func (s *Scanner) deactivateOrDeleteBlockDevices(oldBds map[string]*diskv1.BlockDevice) error {
	for _, oldBd := range oldBds {
		// It should be fine for devices that aren't actually provisioned to go away
		// (this actually works really nicely if the scanner has found individual
		// raw devices that should be part of a multipath device but multipathd
		// isn't running -- once the multipath device activates later, these
		// "wrong" devices just go away).
		if oldBd.Status.ProvisionPhase == diskv1.ProvisionPhaseUnprovisioned {
			logrus.Debugf("Delete device %s", oldBd.Name)
			if err := s.Blockdevices.Delete(oldBd.Namespace, oldBd.Name, &metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
				return err
			}
			continue
		}
		// ..but devices that _are_ provisioned need to be set inactive
		// (see https://github.com/harvester/node-disk-manager/pull/55 for history)
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

// reloadConfigMapFilters reloads filter and auto-provision configurations from ConfigMap
// Falls back to environment variables if ConfigMap is not available or empty
func (s *Scanner) loadConfigMapFilters(ctx context.Context) {
	deviceFilter, vendorFilter, pathFilter, labelFilter, err := s.ConfigMapLoader.LoadFiltersFromConfigMap(ctx)
	if err != nil {
		logrus.Warnf("Failed to reload filters from ConfigMap: %v, using environment variable fallback", err)
		deviceFilter, vendorFilter, pathFilter, labelFilter = s.ConfigMapLoader.GetEnvFilters()
	} else if deviceFilter == "" && vendorFilter == "" && pathFilter == "" && labelFilter == "" {
		// ConfigMap exists but is empty, use env var fallback
		logrus.Info("ConfigMap filter data is empty, using environment variable fallback")
		deviceFilter, vendorFilter, pathFilter, labelFilter = s.ConfigMapLoader.GetEnvFilters()
	}

	// Update filters
	s.ExcludeFilters = filter.SetExcludeFilters(deviceFilter, vendorFilter, pathFilter, labelFilter)

	autoProvisionFilter, err := s.ConfigMapLoader.LoadAutoProvisionFromConfigMap(ctx)
	if err != nil {
		logrus.Warnf("Failed to reload auto-provision from ConfigMap: %v, using environment variable fallback", err)
		autoProvisionFilter = s.ConfigMapLoader.GetEnvAutoProvisionFilter()
	} else if autoProvisionFilter == "" {
		// ConfigMap exists but is empty, use env var fallback
		logrus.Debug("ConfigMap auto-provision data is empty, using environment variable fallback")
		autoProvisionFilter = s.ConfigMapLoader.GetEnvAutoProvisionFilter()
	}

	// Update auto-provision filters
	s.AutoProvisionFilters = filter.SetAutoProvisionFilters(autoProvisionFilter)
}

// scanBlockDevicesOnNode scans block devices on the node, and it will either create or update them.
func (s *Scanner) scanBlockDevicesOnNode(ctx context.Context) error {
	logrus.WithFields(logrus.Fields{
		"node": s.NodeName,
	}).Debug("Scanning block devices")

	// load filter and auto-provision configurations from ConfigMap
	s.loadConfigMapFilters(ctx)

	// List all the block devices. These won't have valid names.
	allDevices := s.collectAllDevices()

	// The list of old block devices (i.e. existing BD CRs) _will_ have valid names.
	existingBDs, err := s.Blockdevices.List(s.Namespace, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", corev1.LabelHostname, s.NodeName),
	})
	if err != nil {
		return err
	}

	existingBDsByName, existingBDsByWWN, existingBDsByUUID := mapBlockDeviceIDs(existingBDs)
	for _, device := range allDevices {
		newBd := device.bd
		autoProvisioned := device.AutoProvisioned

		var existingBd *diskv1.BlockDevice = nil

		// Here's where we find the BD by "what's on the disk"...
		// The identify order is:
		// 1. UUID (for provisioned disks)
		// 2. WWN if there's no UUID
		// 3. Vendor+Model+Serial+BusPath if there's no UUID or WWN
		if uuid, uuidValid := getBlockDeviceUUID(newBd); uuidValid {
			if foundBd, uuidExists := existingBDsByUUID[uuid]; uuidExists {
				logrus.WithFields(logrus.Fields{
					"device": newBd.Status.DeviceStatus.DevPath,
					"uuid":   uuid,
					"name":   foundBd.Name,
				}).Debug("found existing BD by UUID")
				existingBd = foundBd
			}
			// If it has a UUID but isn't in existingBDsByUUID, this will
			// fall through to try and create a new BD, unless it gets
			// picked up by WWN, or vendor+model+serial+buspath
		}

		if wwn, wwnValid := getBlockDeviceWWN(newBd); wwnValid && existingBd == nil {
			if foundBd, wwnExists := existingBDsByWWN[wwn]; wwnExists {
				logrus.WithFields(logrus.Fields{
					"device": newBd.Status.DeviceStatus.DevPath,
					"wwn":    wwn,
					"name":   foundBd.Name,
				}).Debug("found existing BD by WWN")
				existingBd = foundBd
			}
			// If it has a WWN but isn't in existingBDsByWWN, this will
			// fall through to try and create a new BD, unless it
			// gets picked up by vendor+model+serial+buspath
		}

		if existingBd == nil {
			// We have neither UUID nor WWN, so fall back to matching
			// vendor+model+serial+buspath.  If these four match, it's
			// the same device.  I don't think we can rely on any of
			// these individually:
			// - vendor and model are too broad (there can be many devices
			//   for which these are the same)
			// - vendor+model+serial should theoretically be unique, except:
			//   - we've seen some RAID controllers expose duplicate serials
			//   - this doesn't help for virtio disks which have no model
			//     nor serial
			// This method will result in additional BDs being added if
			// someone pops the case and physically moves disks around,
			// but there's probably no fixing that.
			for _, foundBd := range existingBDs.Items {
				if foundBd.Status.DeviceStatus.Details.Vendor == newBd.Status.DeviceStatus.Details.Vendor &&
					foundBd.Status.DeviceStatus.Details.Model == newBd.Status.DeviceStatus.Details.Model &&
					foundBd.Status.DeviceStatus.Details.SerialNumber == newBd.Status.DeviceStatus.Details.SerialNumber &&
					foundBd.Status.DeviceStatus.Details.BusPath == newBd.Status.DeviceStatus.Details.BusPath {
					logrus.WithFields(logrus.Fields{
						"device":  newBd.Status.DeviceStatus.DevPath,
						"vendor":  newBd.Status.DeviceStatus.Details.Vendor,
						"model":   newBd.Status.DeviceStatus.Details.Model,
						"serial":  newBd.Status.DeviceStatus.Details.SerialNumber,
						"buspath": newBd.Status.DeviceStatus.Details.BusPath,
						"name":    foundBd.Name,
					}).Debug("found existing BD by Vendor+Model+SerialNumber+BusPath")
					existingBd = &foundBd
					break
				}
			}
		}

		if existingBd != nil {
			// Pick up the name of the existing block device we found (not strictly necessary,
			// but just in case we try to use newBd.name in handleExistingDev...)
			newBd.Name = existingBd.Name
			if s.handleExistingDev(existingBd, newBd, autoProvisioned) {
				// only first time to update the cache
				if !CacheDiskTags.Initialized() && existingBd.Spec.Tags != nil && len(existingBd.Spec.Tags) > 0 {
					CacheDiskTags.UpdateDiskTags(existingBd.Name, existingBd.Spec.Tags)
				}
				// remove blockdevice from list of existing device names so we can delete missing devices afterward
				delete(existingBDsByName, newBd.Name)
			}
		} else {
			// New block device, needs a name...
			newBd.Name = uuid.NewString()

			logrus.WithFields(logrus.Fields{
				"name":    newBd.Name,
				"device":  newBd.Status.DeviceStatus.DevPath,
				"vendor":  newBd.Status.DeviceStatus.Details.Vendor,
				"model":   newBd.Status.DeviceStatus.Details.Model,
				"serial":  newBd.Status.DeviceStatus.Details.SerialNumber,
				"buspath": newBd.Status.DeviceStatus.Details.BusPath,
				"uuid":    newBd.Status.DeviceStatus.Details.UUID,
				"wwn":     newBd.Status.DeviceStatus.Details.WWN,
			}).Info("creating new BD")
			if _, err := s.SaveBlockDevice(newBd, autoProvisioned); err != nil && !errors.IsAlreadyExists(err) {
				return err
			}
			// Add newly added disk to existingUUID and existingWWN maps in case there's
			// any other disks to be added which somehow have duplicate UUIDs or WWNs.
			if uuid, uuidValid := getBlockDeviceUUID(newBd); uuidValid {
				existingBDsByUUID[uuid] = newBd
			}
			if wwn, wwnValid := getBlockDeviceWWN(newBd); wwnValid {
				existingBDsByWWN[wwn] = newBd
			}
		}
	}
	if !CacheDiskTags.Initialized() {
		CacheDiskTags.UpdateInitialized()
		logrus.Debugf("CacheDiskTags initialized: %+v", CacheDiskTags)
	}

	if err := s.deactivateOrDeleteBlockDevices(existingBDsByName); err != nil {
		return err
	}
	return nil
}

func getBlockDeviceWWN(bd *diskv1.BlockDevice) (string, bool) {
	// WWN should always either be valid or "unknown", but doesn't hurt to also check for an empty string
	return bd.Status.DeviceStatus.Details.WWN, bd.Status.DeviceStatus.Details.WWN != "" && bd.Status.DeviceStatus.Details.WWN != util.UNKNOWN
}

func getBlockDeviceUUID(bd *diskv1.BlockDevice) (string, bool) {
	// UUID should always be either valid or an empty string, but doesn't hurt to also check for "unknown"
	return bd.Status.DeviceStatus.Details.UUID, bd.Status.DeviceStatus.Details.UUID != "" && bd.Status.DeviceStatus.Details.UUID != util.UNKNOWN
}

// mapBlockDeviceIDs returns three maps:
// - names maps BD names to BlockDevices
// - wwns maps device WWNs to BlockDevices
// - uuids maps filesystem/LVM UUIDs to BlockDevices
// The names map will contain all existing BDs, but the wwns and uuids maps will
// only contain BDs that actually have WWNs and UUIDs respectively.
func mapBlockDeviceIDs(bdList *diskv1.BlockDeviceList) (names map[string]*diskv1.BlockDevice, wwns map[string]*diskv1.BlockDevice, uuids map[string]*diskv1.BlockDevice) {
	names = make(map[string]*diskv1.BlockDevice)
	wwns = make(map[string]*diskv1.BlockDevice)
	uuids = make(map[string]*diskv1.BlockDevice)
	for _, bd := range bdList.Items {
		names[bd.Name] = &bd
		if wwn, ok := getBlockDeviceWWN(&bd); ok {
			wwns[wwn] = &bd
		}
		if uuid, ok := getBlockDeviceUUID(&bd); ok {
			uuids[uuid] = &bd
		}
	}
	return
}

// ApplyExcludeFiltersForDisk check the status of disk for every
// registered exclude filters. If the disk meets one of the criteria, it
// returns true.
func (s *Scanner) ApplyExcludeFiltersForDisk(disk *block.Disk) bool {
	if strings.HasPrefix(disk.Name, "dm-") {
		if _, err := utils.IsMultipathDevice(disk.Name); err == nil {
			logrus.Infof("accept block device /dev/%s because it's a multipath device", disk.Name)
			return false
		}

		logrus.Infof("block device /dev/%s ignored because it's a dm device (likely LHv2 volume)", disk.Name)
		return true
	}

	for _, filter := range s.ExcludeFilters {
		if filter.ApplyDiskFilter(disk) {
			logrus.Infof("block device /dev/%s ignored by %s and rules: %s", disk.Name, filter.Name, filter.DiskFilter.Details())
			return true
		}
	}

	if _, err := utils.IsManagedByMultipath(disk.Name); err == nil {
		logrus.Infof("block device /dev/%s is managed by multipath device, ignored", disk.Name)
		return true
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
