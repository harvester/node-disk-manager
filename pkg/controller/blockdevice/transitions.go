package blockdevice

import (
	"fmt"
	"reflect"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/indexers"
	"github.com/harvester/node-disk-manager/pkg/util"
)

const (
	maxConcurrentFormatEffect = 5
)

// transitionTable defines what phase the state manchine will move to
type transitionTable struct {
	namespace        string
	nodeName         string
	nodes            ctllonghornv1.NodeClient
	nodeCache        ctllonghornv1.NodeCache
	blockdevices     ctldiskv1.BlockDeviceClient
	blockdeviceCache ctldiskv1.BlockDeviceCache
	blockInfo        block.Info
}

func newTransitionTable(
	namspace,
	nodeName string,
	bdCache ctldiskv1.BlockDeviceCache,
	nodes ctllonghornv1.NodeClient,
	nodeCache ctllonghornv1.NodeCache,
	blockInfo block.Info,
) transitionTable {
	return transitionTable{
		namespace:        namspace,
		nodeName:         nodeName,
		nodes:            nodes,
		nodeCache:        nodeCache,
		blockdeviceCache: bdCache,
		blockInfo:        blockInfo,
	}
}

// next deduces the next phase from current blockdevice status
func (p transitionTable) next(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	methodName := fmt.Sprintf("Phase%s", currentPhase)
	args := []reflect.Value{reflect.ValueOf(bd)}
	method := reflect.ValueOf(p).MethodByName(methodName)
	if !method.IsValid() {
		err := fmt.Errorf("Unrecognizable phase %s for block device %s", currentPhase, bd.Name)
		return diskv1.ProvisionPhaseUnprovisioned, nil, err
	}
	logrus.Debugf("[Transition] calling %s with device %s", methodName, bd.Name)
	ret := method.Call(args)
	phase := ret[0].Interface().(diskv1.BlockDeviceProvisionPhase)
	effect := ret[1].Interface().(effect)
	var err error
	if !ret[2].IsNil() {
		err = ret[2].Interface().(error)
	}
	return phase, effect, err
}

func (p transitionTable) PhaseUnprovisioned(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	forceFormatted := bd.Spec.FileSystem.ForceFormatted
	switch bd.Status.DeviceStatus.Details.DeviceType {
	case diskv1.DeviceTypeDisk:
		if forceFormatted && !diskv1.DevicePartitioned.IsTrue(bd) {
			// Perform force partition/format
			return diskv1.ProvisionPhasePartitioning, effectGptPartition, nil
		}
		if bd.Status.DeviceStatus.Partitioned {
			// already partitioned before the resource initialization
			return diskv1.ProvisionPhasePartitioned, nil, nil
		}
		// already partitioned during this resource's lifecycle
		return currentPhase, nil, nil
	case diskv1.DeviceTypePart:
		if forceFormatted && !diskv1.DeviceFormatted.IsTrue(bd) {
			key := indexers.MakeDeviceByPhaseKey(bd, diskv1.ProvisionPhaseFormatting)
			bds, err := p.blockdeviceCache.GetByIndex(indexers.DeviceByPhaseIndex, key)
			if err != nil && !errors.IsNotFound(err) {
				return currentPhase, nil, err
			}
			// Exceed maximum number of concurrent effects. Re-enqueue.
			if len(bds) >= maxConcurrentFormatEffect {
				return currentPhase, effectEnqueueCurrentPhase, nil
			}
			// Perform force partition/format
			return diskv1.ProvisionPhaseFormatting, effectFormatPartition, nil
		}
		if bd.Status.DeviceStatus.Details.UUID != "" {
			// already formatted before the resource initialization
			return diskv1.ProvisionPhaseFormatted, nil, nil
		}
		// already formatted during this resource's lifecycle
		return currentPhase, nil, nil
	default:
		return currentPhase, nil, nil
	}
}

func (p transitionTable) PhasePartitioning(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, nil, nil
}

func (p transitionTable) PhasePartitioned(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase

	if !bd.Spec.FileSystem.ForceFormatted {
		return currentPhase, nil, nil
	}

	devPath := util.GetDiskPartitionPath(bd.Spec.DevPath, 1)
	part := p.blockInfo.GetPartitionByDevPath(bd.Spec.DevPath, devPath)
	name := block.GeneratePartitionGUID(part, bd.Spec.NodeName)
	partBd, err := p.blockdeviceCache.Get(bd.Namespace, name)
	if err != nil {
		if errors.IsNotFound(err) {
			return currentPhase, effectEnqueueCurrentPhase, nil
		}
		return currentPhase, nil, err
	}
	return currentPhase, effectPrepareFormatPartitionFactory(partBd), nil
}

func (p transitionTable) PhaseFormatting(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, nil, nil
}

func (p transitionTable) PhaseFormatted(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase

	if bd.Spec.FileSystem.Provisioned {
		filesystem := p.blockInfo.GetFileSystemInfoByDevPath(bd.Spec.DevPath)
		targetMountPoint := util.GetMountPoint(bd.Name)
		mountPointSynced := targetMountPoint == filesystem.MountPoint

		if mountPointSynced {
			return diskv1.ProvisionPhaseMounted, nil, nil
		}

		if filesystem.MountPoint != "" {
			// Unmount old dangling mount point
			return diskv1.ProvisionPhaseUnmounting, effectUnmountFilesystemFactory(filesystem), nil
		}

		return diskv1.ProvisionPhaseMounting, effectMountFilesystem, nil
	}

	return currentPhase, nil, nil
}

func (p transitionTable) PhaseMounting(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, nil, nil
}

func (p transitionTable) PhaseMounted(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase

	filesystem := p.blockInfo.GetFileSystemInfoByDevPath(bd.Spec.DevPath)
	if !bd.Spec.FileSystem.Provisioned && filesystem.MountPoint != "" {
		// Unmount old dangling mount point
		return diskv1.ProvisionPhaseUnmounting, effectUnmountFilesystemFactory(filesystem), nil
	}

	if filesystem.MountPoint == "" {
		// Not mounted yet. Back to previous phase.
		return diskv1.ProvisionPhaseFormatted, nil, nil
	} else if util.GetMountPoint(bd.Name) != filesystem.MountPoint {
		// Unmount old dangling mount point
		return diskv1.ProvisionPhaseUnmounting, effectUnmountFilesystemFactory(filesystem), nil
	}

	node, err := p.getNode()
	if err != nil {
		// NDM might be deployed earlier than Longhorn, so we re-enqueue to wait fot it.
		if errors.IsNotFound(err) {
			return currentPhase, effectEnqueueCurrentPhase, nil
		}
		return currentPhase, nil, err
	}

	return diskv1.ProvisionPhaseProvisioning, effectProvisionDeviceFactory(node), nil
}

func (p transitionTable) PhaseUnmounting(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, nil, nil
}

func (p transitionTable) PhaseProvisioning(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, nil, nil
}

func (p transitionTable) PhaseProvisioned(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase

	node, err := p.getNode()
	if err != nil {
		// NDM might be deployed earlier than Longhorn, so we re-enqueue to wait fot it.
		if errors.IsNotFound(err) {
			return currentPhase, effectEnqueueCurrentPhase, nil
		}
		return currentPhase, nil, err
	}

	if !bd.Spec.FileSystem.Provisioned {
		return diskv1.ProvisionPhaseUnprovisioning, effectUnprovisionDeviceFactory(node), nil
	}

	filesystem := p.blockInfo.GetFileSystemInfoByDevPath(bd.Spec.DevPath)
	if filesystem.MountPoint == "" {
		// Not mounted yet. Back to previous phase.
		return diskv1.ProvisionPhaseFormatted, nil, nil
	}

	disk, ok := node.Spec.Disks[bd.Name]
	if !ok {
		// Not provisioned yet. Back to previous phase.
		return diskv1.ProvisionPhaseMounted, nil, nil
	}

	if disk.Path != util.GetMountPoint(bd.Name) {
		return diskv1.ProvisionPhaseUnprovisioning, effectUnprovisionDeviceFactory(node), nil
	}

	return currentPhase, nil, nil
}

func (p transitionTable) PhaseUnprovisioning(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, nil, nil
}

func (p transitionTable) PhaseFailed(bd *diskv1.BlockDevice) (diskv1.BlockDeviceProvisionPhase, effect, error) {
	currentPhase := bd.Status.ProvisionPhase
	return currentPhase, nil, nil
}

// getNode is for internal use only
func (p transitionTable) getNode() (*longhornv1.Node, error) {
	node, err := p.nodeCache.Get(p.namespace, p.nodeName)
	if err != nil && errors.IsNotFound(err) {
		node, err = p.nodes.Get(p.namespace, p.nodeName, metav1.GetOptions{})
	}
	return node, err
}
