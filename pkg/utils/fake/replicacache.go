package fake

import (
	lhv1beta2 "github.com/harvester/harvester/pkg/generated/controllers/longhorn.io/v1beta2"
	lhv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	"github.com/rancher/wrangler/v3/pkg/generic"
	"k8s.io/apimachinery/pkg/labels"
)

type FakeReplicaCache struct {
	replicas []*lhv1.Replica
}

func NewReplicaCache(replicasToServe []*lhv1.Replica) lhv1beta2.ReplicaCache {
	return &FakeReplicaCache{
		replicas: replicasToServe,
	}
}

func (f *FakeReplicaCache) AddIndexer(indexName string, indexer generic.Indexer[*lhv1.Replica]) {
}

func (f *FakeReplicaCache) Get(namespace string, name string) (*lhv1.Replica, error) {
	panic("unimplemented")
}

func (f *FakeReplicaCache) GetByIndex(indexName string, key string) ([]*lhv1.Replica, error) {
	return f.replicas, nil
}

func (f *FakeReplicaCache) List(namespace string, selector labels.Selector) ([]*lhv1.Replica, error) {
	panic("unimplemented")
}
