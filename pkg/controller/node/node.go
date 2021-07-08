package node

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	longhornv1 "github.com/longhorn/node-disk-manager/pkg/apis/longhorn.io/v1beta1"
	ctllonghornv1 "github.com/longhorn/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/longhorn/node-disk-manager/pkg/option"
)

type Controller struct {
	namespace string

	BlockDevices     ctllonghornv1.BlockDeviceController
	BlockDeviceCache ctllonghornv1.BlockDeviceCache
	Nodes            ctllonghornv1.NodeController
}

const (
	blockDeviceNodeHandlerName = "longhorn-ndm-node-handler"
)

// Register register the longhorn node CRD controller
func Register(ctx context.Context, nodes ctllonghornv1.NodeController, bds ctllonghornv1.BlockDeviceController, opt *option.Option) error {

	c := &Controller{
		namespace:        opt.Namespace,
		Nodes:            nodes,
		BlockDevices:     bds,
		BlockDeviceCache: bds.Cache(),
	}

	nodes.OnRemove(ctx, blockDeviceNodeHandlerName, c.OnNodeDelete)
	return nil
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
