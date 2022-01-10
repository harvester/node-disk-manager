package blockdevice

import (
	"context"
	"fmt"
	"time"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/disk"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/option"
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

	transitionTable transitionTable
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
		transitionTable:  newTransitionTable(opt.Namespace, opt.NodeName, bds, nodes, scanner),
	}

	if err := controller.scanner.StartScanning(); err != nil {
		return err
	}

	bds.OnChange(ctx, blockDeviceHandlerName, controller.OnBlockDeviceChange)
	bds.OnRemove(ctx, blockDeviceHandlerName, controller.OnBlockDeviceDelete)
	return nil
}

func (c *Controller) updateFailed(device *diskv1.BlockDevice, err error) (*diskv1.BlockDevice, error) {
	logrus.Error(err)
	deviceCpy := device.DeepCopy()
	setDeviceFailedCondition(deviceCpy, corev1.ConditionTrue, err.Error())
	diskv1.ProvisionPhaseFailed.Set(deviceCpy)
	return c.blockdevices.Update(deviceCpy)
}

func (c *Controller) OnBlockDeviceChange(key string, device *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	if device == nil || device.DeletionTimestamp != nil || device.Spec.NodeName != c.nodeName {
		return nil, nil
	}

	// Get next phase from previous.
	phase, effect, err := c.transitionTable.next(device)
	if err != nil {
		err := fmt.Errorf("[Transition] Failed to determine next phase of %s for %s: %v", device.Status.ProvisionPhase, device.Name, err)
		return c.updateFailed(device, err)
	}

	if phase == device.Status.ProvisionPhase {
		// No need to transit if the phases are the same.
		logrus.Debugf("[Transition] Already in phase %s for %s", device.Status.ProvisionPhase, device.Name)
	} else {
		var err error
		deviceCpy := device.DeepCopy()
		phase.Set(deviceCpy)
		logrus.Debugf("[Transition] from %s to %s", device.Status.ProvisionPhase, deviceCpy.Status.ProvisionPhase)
		device, err = c.blockdevices.Update(deviceCpy)
		if err != nil {
			err := fmt.Errorf("[Transition] Failed to transit from %s to %s for %s: %v", device.Status.ProvisionPhase, phase, device.Name, err)
			return c.updateFailed(device, err)
		}
	}

	// Emit side effect
	//
	// Note that NDM always tries to emit side effect here whether or not a
	// transition is needed or not. Phase transition should pass a no-op
	// side effect if they aim not to.
	if err := effect(c, device); err != nil {
		err := fmt.Errorf("[Transition] Failed to emit effect on phase %s for device %s: %v", device.Status.ProvisionPhase, device.Name, err)
		return nil, err
	}

	return nil, nil
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
				return nil, fmt.Errorf("cannot unmount disk %s from mount point %s, err: %s", bd.Name, filesystem.MountPoint, err.Error())
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

// effectContrller interface

func (c *Controller) Nodes() ctllonghornv1.NodeClient {
	return c.nodes
}

func (c *Controller) NodeCache() ctllonghornv1.NodeCache {
	return c.nodeCache
}

func (c *Controller) Blockdevices() ctldiskv1.BlockDeviceController {
	return c.blockdevices
}

func (c *Controller) BlockdeviceCache() ctldiskv1.BlockDeviceCache {
	return c.blockdeviceCache
}
