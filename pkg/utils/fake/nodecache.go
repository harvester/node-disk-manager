package fake

import (
	ctlcorev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/v3/pkg/generic"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FakeNodeCache struct {
	nodes []*v1.Node
}

func NewNodeCache(nodesToServe []*v1.Node) ctlcorev1.NodeCache {
	return &FakeNodeCache{
		nodes: nodesToServe,
	}
}

func (c *FakeNodeCache) AddIndexer(indexName string, indexer generic.Indexer[*v1.Node]) {
	panic("unimplemented")
}

func (c *FakeNodeCache) Get(name string) (*v1.Node, error) {
	for _, node := range c.nodes {
		if node.Name == name {
			return node.DeepCopy(), nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

func (c *FakeNodeCache) GetByIndex(indexName, key string) ([]*v1.Node, error) {
	panic("unimplemented")

}

func (c *FakeNodeCache) List(selector labels.Selector) ([]*v1.Node, error) {
	var matchingNodes []*v1.Node
	for _, node := range c.nodes {
		if selector.Matches(labels.Set(node.GetLabels())) {
			matchingNodes = append(matchingNodes, node.DeepCopy())
		}
	}
	return matchingNodes, nil
}
