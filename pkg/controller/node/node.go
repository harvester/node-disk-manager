package node

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/util"
	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	"github.com/sirupsen/logrus"
)

type Controller struct {
	namespace string

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
		Nodes:            nodes,
		BlockDevices:     bds,
		BlockDeviceCache: bds.Cache(),
	}

	nodes.OnChange(ctx, blockDeviceNodeHandlerName, c.OnNodeChange)
	nodes.OnRemove(ctx, blockDeviceNodeHandlerName, c.OnNodeDelete)
	return nil
}

func (c *Controller) OnNodeChange(key string, node *longhornv1.Node) (*longhornv1.Node, error) {
	if node == nil || node.DeletionTimestamp != nil {
		return nil, nil
	}

	toRemove := make([]string, 0)
	nodeCpy := node.DeepCopy()
	for name, disk := range node.Spec.Disks {
		needRemove := false
		for _, tag := range disk.Tags {
			if tag == util.DiskRemoveTag {
				needRemove = true
				break
			}
		}
		if needRemove {
			if status, ok := node.Status.DiskStatus[name]; ok && len(status.ScheduledReplica) == 0 {
				delete(nodeCpy.Spec.Disks, name)
				toRemove = append(toRemove, name)
			}
		}
	}

	if len(toRemove) > 0 {
		if _, err := c.Nodes.Update(nodeCpy); err != nil {
			return nil, err
		}

		for _, name := range toRemove {
			logrus.Debugf("Disk %s was removed from node %s", name, node.Name)
			c.BlockDevices.Enqueue(c.namespace, name)
		}
	}

	return node, nil
}

// OnNodeDelete watch the node CR on remove and delete node related block devices
func (c *Controller) OnNodeDelete(key string, node *longhornv1.Node) (*longhornv1.Node, error) {
	if node == nil {
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
