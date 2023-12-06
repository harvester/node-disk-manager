package node

import (
	"context"
	"reflect"
	"strings"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta2"
	"github.com/harvester/node-disk-manager/pkg/option"
)

type Controller struct {
	namespace string
	nodeName  string

	BlockDevices     ctldiskv1.BlockDeviceController
	BlockDeviceCache ctldiskv1.BlockDeviceCache
	Nodes            ctllonghornv1.NodeController
}

const (
	blockDeviceNodeHandlerName = "harvester-ndm-node-handler"
)

// Register register the longhorn node CRD controller
func Register(ctx context.Context, nodes ctllonghornv1.NodeController, bds ctldiskv1.BlockDeviceController, opt *option.Option) error {

	c := &Controller{
		namespace:        opt.Namespace,
		nodeName:         opt.NodeName,
		Nodes:            nodes,
		BlockDevices:     bds,
		BlockDeviceCache: bds.Cache(),
	}

	nodes.OnChange(ctx, blockDeviceNodeHandlerName, c.OnNodeChange)
	nodes.OnRemove(ctx, blockDeviceNodeHandlerName, c.OnNodeDelete)
	return nil
}

// OnChange watch the node CR on change and sync up to block device CR
func (c *Controller) OnNodeChange(_ string, node *longhornv1.Node) (*longhornv1.Node, error) {
	if node == nil || node.DeletionTimestamp != nil {
		logrus.Debugf("Skip this round because the node will be deleted or not created")
		return nil, nil
	}
	if c.nodeName != node.Name {
		logrus.Debugf("Skip this round because the CRD node name %s is not belong to this node %s", node.Name, c.nodeName)
		return nil, nil
	}

	for name, disk := range node.Spec.Disks {
		// default disk does not included in block device CR
		if strings.HasPrefix(name, "default-disk") {
			continue
		}

		logrus.Debugf("Prepare to checking block device %s", name)
		bd, err := c.BlockDevices.Get(c.namespace, name, metav1.GetOptions{})
		if err != nil {
			logrus.Warnf("Get block device %s failed: %v", name, err)
			return node, err
		}

		bdCpy := bd.DeepCopy()
		bdCpy.Status.Tags = disk.Tags

		if !reflect.DeepEqual(bd, bdCpy) {
			logrus.Debugf("Update block device %s tags (Status) from %v to %v", bd.Name, bd.Status.Tags, disk.Tags)
			if _, err := c.BlockDevices.Update(bdCpy); err != nil {
				logrus.Warnf("Update block device %s failed: %v", bd.Name, err)
				return node, err
			}
		}
	}
	return nil, nil
}

// OnNodeDelete watch the node CR on remove and delete node related block devices
func (c *Controller) OnNodeDelete(_ string, node *longhornv1.Node) (*longhornv1.Node, error) {
	if node == nil || node.DeletionTimestamp == nil {
		return nil, nil
	}

	bds, err := c.BlockDeviceCache.List(c.namespace, labels.SelectorFromSet(map[string]string{
		v1.LabelHostname: node.Name,
	}))
	if err != nil {
		return node, err
	}

	for _, bd := range bds {
		if err := c.BlockDevices.Delete(c.namespace, bd.Name, &metav1.DeleteOptions{}); err != nil {
			return node, err
		}
	}
	return nil, nil
}
