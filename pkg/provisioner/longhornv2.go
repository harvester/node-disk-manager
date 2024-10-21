package provisioner

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	"github.com/sirupsen/logrus"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta2"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type LonghornV2Provisioner struct {
	*LonghornV1Provisioner
}

func NewLHV2Provisioner(
	device *diskv1.BlockDevice,
	block block.Info,
	nodeObj *longhornv1.Node,
	nodesClient ctllonghornv1.NodeClient,
	nodesClientCache ctllonghornv1.NodeCache,
	cacheDiskTags *DiskTags,
) (Provisioner, error) {
	if !cacheDiskTags.Initialized() {
		return nil, errors.New(ErrorCacheDiskTagsNotInitialized)
	}
	if device.Spec.Provisioner.Longhorn.DiskDriver == longhornv1.DiskDriverNone {
		// We need to force DiskDriver to "auto" if it's not explicitly set,
		// because Longhorn also does that internally.  If we don't do it
		// here, the subsequent reflect.DeepEqual() in our Update() function
		// will always fail because we have an empty string, but the LHN CR
		// has it set to "auto" which results in a weird resync loop.
		device.Spec.Provisioner.Longhorn.DiskDriver = longhornv1.DiskDriverAuto
	}
	baseProvisioner := &provisioner{
		name:      TypeLonghornV2,
		blockInfo: block,
		device:    device,
	}
	return &LonghornV2Provisioner{
		&LonghornV1Provisioner{
			provisioner:      baseProvisioner,
			nodeObj:          nodeObj,
			nodesClient:      nodesClient,
			nodesClientCache: nodesClientCache,
			cacheDiskTags:    cacheDiskTags,
			semaphoreObj:     nil,
		},
	}, nil
}

// Format should really be a no-op for V2 disks given they just take the
// whole device, but for NVMe devices where Longhorn decides to use the
// nvme bdev driver, device activation will fail if there's an existing
// filesystem on the device, so we need to make sure to wipe before use.
func (p *LonghornV2Provisioner) Format(devPath string) (isFormatComplete, isRequeueNeeded bool, err error) {
	if _, err = utils.NewExecutor().Execute("wipefs", []string{"-a", devPath}); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// UnFormat is a no-op for V2 disks
func (p *LonghornV2Provisioner) UnFormat() (isRequeueNeeded bool, err error) {
	return
}

// Provision adds the block device to longhorn's list of disks.  Longhorn's admission
// webhook will deny the update if the V2 engine isn't enabled (you'll see something
// like 'admission webhook "validator.longhorn.io" denied the request: update disk on
// node harvester-node-0 error: The disk 989754e4e66edadfd3390974a1aba3f8(/dev/sda) is
// a block device, but the SPDK feature is not enabled')
func (p *LonghornV2Provisioner) Provision() (isRequeueNeeded bool, err error) {
	logrus.WithFields(logrus.Fields{
		"provisioner": p.name,
		"device":      p.device.Name,
	}).Info("Provisioning Longhorn block device")

	nodeObjCpy := p.nodeObj.DeepCopy()
	tags := []string{}
	if p.device.Spec.Tags != nil {
		tags = p.device.Spec.Tags
	}

	// Here we want either a BDF path, or a /dev/disk/by-id/ path (not the short
	// path like /dev/sdx which might change)
	devPath, err := resolveLonghornV2DevPath(p.device)
	if err != nil {
		// Probably no point requeuing if we can't resolve the device path,
		// because really what's going to change between this call and the next...?
		return false, err
	}

	diskSpec := longhornv1.DiskSpec{
		Type:              longhornv1.DiskTypeBlock,
		Path:              devPath,
		AllowScheduling:   true,
		EvictionRequested: false,
		StorageReserved:   0,
		Tags:              tags,
		DiskDriver:        p.device.Spec.Provisioner.Longhorn.DiskDriver,
	}

	// We're intentionally not trying to sync disk tags from longhorn if the
	// disk already exists, because the Harvester blockdevice CR is meant to
	// be the source of truth.

	nodeObjCpy.Spec.Disks[p.device.Name] = diskSpec
	if !reflect.DeepEqual(p.nodeObj, nodeObjCpy) {
		if _, err := p.nodesClient.Update(nodeObjCpy); err != nil {
			return true, err
		}
	}

	if !diskv1.DiskAddedToNode.IsTrue(p.device) {
		msg := fmt.Sprintf("Added disk %s to longhorn node `%s` as an additional disk", p.device.Name, p.nodeObj.Name)
		setCondDiskAddedToNodeTrue(p.device, msg, diskv1.ProvisionPhaseProvisioned)
	}

	p.cacheDiskTags.UpdateDiskTags(p.device.Name, p.device.Spec.Tags)
	return
}

func (p *LonghornV2Provisioner) UnProvision() (isRequeueNeeded bool, err error) {
	// The LH v1 UnProvision function works just fine for unprovisioning
	// a V2 volume.  The only thing it does that's not necessary for V2
	// is the potential call to unmountTheBrokenDisk(), but that doesn't
	// do anything if there's no filesystem mounted, so it's a no-op.
	return p.LonghornV1Provisioner.UnProvision()
}

func (p *LonghornV2Provisioner) Update() (isRequeueNeeded bool, err error) {
	// Sync disk tags (we can just use the V1 implementation for this for now)
	isRequeueNeeded, err = p.LonghornV1Provisioner.Update()
	if err != nil {
		return
	}

	// Sync disk driver
	if targetDisk, found := p.nodeObj.Spec.Disks[p.device.Name]; found {
		nodeObjCpy := p.nodeObj.DeepCopy()
		targetDisk.DiskDriver = p.device.Spec.Provisioner.Longhorn.DiskDriver
		nodeObjCpy.Spec.Disks[p.device.Name] = targetDisk
		if !reflect.DeepEqual(p.nodeObj, nodeObjCpy) {
			if _, err = p.nodesClient.Update(nodeObjCpy); err != nil {
				isRequeueNeeded = true
			}
		}
	}
	return
}

// resolveLonghornV2DevPath will return a BDF path if possible for virtio or
// NVMe devices, then will fall back to /dev/disk/by-id (which requires the
// disk to have a WWN).  For details on BDF pathing, see
// https://longhorn.io/docs/1.7.1/v2-data-engine/features/node-disk-support/
func resolveLonghornV2DevPath(device *diskv1.BlockDevice) (string, error) {
	if device.Status.DeviceStatus.Details.DeviceType != diskv1.DeviceTypeDisk {
		return "", fmt.Errorf("device type must be disk to resolve Longhorn V2 device path (type is %s)",
			device.Status.DeviceStatus.Details.DeviceType)
	}
	devPath := ""
	if device.Status.DeviceStatus.Details.StorageController == string(diskv1.StorageControllerVirtio) ||
		device.Status.DeviceStatus.Details.StorageController == string(diskv1.StorageControllerNVMe) {
		// In both of these cases, we should (hopefully!) be able to extract BDF from BusPath
		if strings.HasPrefix(device.Status.DeviceStatus.Details.BusPath, "pci-") {
			devPath = strings.Split(device.Status.DeviceStatus.Details.BusPath, "-")[1]
		}
		if len(devPath) > 0 {
			return devPath, nil
		}
		logrus.WithFields(logrus.Fields{
			"device":  device.Name,
			"buspath": device.Status.DeviceStatus.Details.BusPath,
		}).Warn("Unable to extract BDF from BusPath, falling back to WWN")
	}
	if wwn := device.Status.DeviceStatus.Details.WWN; valueExists(wwn) {
		devPath = "/dev/disk/by-id/wwn-" + wwn
		_, err := os.Stat(devPath)
		if err == nil {
			return devPath, nil
		}
		logrus.WithFields(logrus.Fields{
			"device": device.Name,
			"wwn":    device.Status.DeviceStatus.Details.WWN,
		}).Warn("/dev/disk/by-id/wwn-* path does not exist for device")
	}
	// TODO: see if we can find something else under /dev/disk/by-id, for
	// example maybe there's a serial number but no WWN.  In the "no WWN"
	// case, maybe it's sufficient to just take whatever path we can find
	// under /dev/disk/by-id that links back to the device...?
	return "", fmt.Errorf("unable to resolve Longhorn V2 device path; %s has no WWN and no BDF", device.Name)
}
