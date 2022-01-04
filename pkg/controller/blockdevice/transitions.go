package blockdevice

import (
	"fmt"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/util"
)

// transitionTable defines what phase the state manchine will move to
type transitionTable struct {
	namespace        string
	nodeName         string
	nodes            ctllonghornv1.NodeClient
	nodeCache        ctllonghornv1.NodeCache
	blockdevices     ctldiskv1.BlockDeviceClient
	blockdeviceCache ctldiskv1.BlockDeviceCache
	scanner          *Scanner
}

func newTransitionTable(
	namspace,
	nodeName string,
	bds ctldiskv1.BlockDeviceController,
	nodes ctllonghornv1.NodeController,
	scanner *Scanner,
) transitionTable {
	blockdeviceCache := bds.Cache()
	return transitionTable{
		namespace:        namspace,
		nodeName:         nodeName,
		nodes:            nodes,
		nodeCache:        nodes.Cache(),
		blockdeviceCache: blockdeviceCache,
		scanner:          scanner,
	}
}

// next deduces the next phase from current blockdevice status
func (p transitionTable) next(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	switch currentPhase {
	case diskv1.ProvisionPhaseUnprovisioned:
		return p.phaseUnprovisioned(bd)
	case diskv1.ProvisionPhasePartitioning:
		return p.phasePartitioning(bd)
	case diskv1.ProvisionPhasePartitioned:
		return p.phasePartitioned(bd)
	case diskv1.ProvisionPhaseFormatting:
		return p.phaseFormatting(bd)
	case diskv1.ProvisionPhaseFormatted:
		return p.phaseFormatted(bd)
	case diskv1.ProvisionPhaseMounting:
		return p.phaseMounting(bd)
	case diskv1.ProvisionPhaseMounted:
		return p.phaseMounted(bd)
	case diskv1.ProvisionPhaseUnmounting:
		return p.phaseUnmounting(bd)
	case diskv1.ProvisionPhaseProvisioning:
		return p.phaseProvisioning(bd)
	case diskv1.ProvisionPhaseProvisioned:
		return p.phaseProvisioned(bd)
	case diskv1.ProvisionPhaseUnprovisioning:
		return p.phaseUnprovisioning(bd)
	case diskv1.ProvisionPhaseFailed:
		return p.phaseFailed(bd)
	default:
		err := fmt.Errorf("Unrecognizable phase %s for block device %s", currentPhase, bd.Name)
		return diskv1.ProvisionPhaseUnprovisioned, noop, err
	}
}

func (p transitionTable) phaseUnprovisioned(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	// Disk only cares about force formatting itself to a single root partition.
	if !bd.Spec.FileSystem.ForceFormatted {
		return currentPhase, noop, nil
	}
	switch bd.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
		if diskv1.DevicePartitioned.IsTrue(bd) {
			// already partitioned
			return currentPhase, noop, nil
		}
		return diskv1.ProvisionPhasePartitioning, effectGptPartition, nil
	case diskv1.DeviceTypePart:
		if diskv1.DeviceFormatted.IsTrue(bd) {
			// already formatted
			return currentPhase, noop, nil
		}
		return diskv1.ProvisionPhaseFormatting, effectFormatPartition, nil
	default:
		return currentPhase, noop, nil
	}
}

func (p transitionTable) phasePartitioning(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, noop, nil
}

func (p transitionTable) phasePartitioned(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	devPath := util.GetDiskPartitionPath(bd.Spec.DevPath, 1)
	part := p.scanner.BlockInfo.GetPartitionByDevPath(bd.Spec.DevPath, devPath)
	name := block.GeneratePartitionGUID(part, bd.Spec.NodeName)
	partBd, err := p.blockdeviceCache.Get(bd.Namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			return currentPhase, effectEnqueuePrepareFormatPartition, nil
		}
		return currentPhase, noop, err
	}
	if !diskv1.ProvisionPhaseUnprovisioned.Matches(partBd) {
		logrus.Debugf("Not an unprovisioned disk %s, skip...", partBd.Name)
		return currentPhase, noop, nil
	}
	return currentPhase, effectPrepareFormatPartition(partBd), nil
}

func (p transitionTable) phaseFormatting(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, noop, nil
}

func (p transitionTable) phaseFormatted(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	filesystem := p.scanner.BlockInfo.GetFileSystemInfoByDevPath(bd.Spec.DevPath)
	fs := *bd.Spec.FileSystem

	mountPointSynced := fs.MountPoint == filesystem.MountPoint

	if mountPointSynced {
		// Mount points are synced.
		// Now check if the disk needs to provision to longhorn.
		if fs.MountPoint != "" {
			return diskv1.ProvisionPhaseMounted, noop, nil
		}
		// No mount point. No need to transit phase.
		return currentPhase, noop, nil
	}

	// Mount points are different.

	if filesystem.MountPoint != "" {
		// If there is old mount point, umount first
		return diskv1.ProvisionPhaseUnmounting, effectUnmountFilesystem(filesystem), nil
	}
	// If there is a new mount point, mount it
	return diskv1.ProvisionPhaseMounting, effectMountFilesystem, nil
}

func (p transitionTable) phaseMounting(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, noop, nil
}

func (p transitionTable) phaseMounted(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	node, err := p.getNode()
	if err != nil {
		return currentPhase, noop, err
	}

	mountPoint := bd.Spec.FileSystem.MountPoint
	disk, ok := node.Spec.Disks[bd.Name]
	if ok && disk.Path != mountPoint {
		return diskv1.ProvisionPhaseUnprovisioning, effectUnprovisionDevice(node, disk), nil
	}
	return diskv1.ProvisionPhaseProvisioning, effectProvisionDevice(node), nil
}

func (p transitionTable) phaseUnmounting(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, noop, nil
}

func (p transitionTable) phaseProvisioning(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, noop, nil
}

func (p transitionTable) phaseProvisioned(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	mountPoint := bd.Spec.FileSystem.MountPoint
	if mountPoint == "" {
		return diskv1.ProvisionPhaseUnprovisioning, noop, nil
	}
	filesystem := p.scanner.BlockInfo.GetFileSystemInfoByDevPath(bd.Spec.DevPath)
	if mountPoint != filesystem.MountPoint {
		return diskv1.ProvisionPhaseUnprovisioning, noop, nil
	}
	return currentPhase, noop, nil
}

func (p transitionTable) phaseUnprovisioning(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	node, err := p.getNode()
	if err != nil {
		return currentPhase, noop, err
	}
	if disk, ok := node.Spec.Disks[bd.Name]; ok {
		return currentPhase, effectUnprovisionDevice(node, disk), nil
	}
	return currentPhase, noop, nil
}

func (p transitionTable) phaseFailed(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, noop, nil
}

// getNode is for internal use only
func (p transitionTable) getNode() (*longhornv1.Node, error) {
	node, err := p.nodeCache.Get(p.namespace, p.nodeName)
	if err != nil && errors.IsNotFound(err) {
		node, err = p.nodes.Get(p.namespace, p.nodeName, metav1.GetOptions{})
	}
	return node, err
}
