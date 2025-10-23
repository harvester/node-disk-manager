package fake

import (
	lhv1beta2 "github.com/harvester/harvester/pkg/generated/controllers/longhorn.io/v1beta2"
	lhv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	"github.com/rancher/wrangler/v3/pkg/generic"
	"k8s.io/apimachinery/pkg/labels"
)

type FakeLonghornNodeCache struct {
	nodes []*lhv1.Node
}

func NewLonghornNodeCache(nodesToServe []*lhv1.Node) lhv1beta2.NodeCache {
	return &FakeLonghornNodeCache{
		nodes: nodesToServe,
	}
}

func (f *FakeLonghornNodeCache) AddIndexer(indexName string, indexer generic.Indexer[*lhv1.Node]) {
}

func (f *FakeLonghornNodeCache) Get(namespace string, name string) (*lhv1.Node, error) {
	return f.nodes[0], nil
}

func (f *FakeLonghornNodeCache) GetByIndex(indexName string, key string) ([]*lhv1.Node, error) {
	return f.nodes, nil
}

func (f *FakeLonghornNodeCache) List(namespace string, selector labels.Selector) ([]*lhv1.Node, error) {
	panic("unimplemented")
}
