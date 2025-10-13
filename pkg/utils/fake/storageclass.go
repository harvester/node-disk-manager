package fake

import (
	ctlstoragev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/storage/v1"
	"github.com/rancher/wrangler/v3/pkg/generic"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FakeStorageClassCache struct {
	scs []*storagev1.StorageClass
}

func NewStorageClassCache(scsToServe []*storagev1.StorageClass) ctlstoragev1.StorageClassCache {
	return &FakeStorageClassCache{
		scs: scsToServe,
	}
}

func (f *FakeStorageClassCache) AddIndexer(indexName string, indexer generic.Indexer[*storagev1.StorageClass]) {
	panic("unimplemented")
}

func (f *FakeStorageClassCache) Get(name string) (*storagev1.StorageClass, error) {
	for _, storageClass := range f.scs {
		if storageClass.Name == name {
			return storageClass.DeepCopy(), nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

func (f *FakeStorageClassCache) GetByIndex(indexName string, key string) ([]*storagev1.StorageClass, error) {
	panic("unimplemented")
}

func (f *FakeStorageClassCache) List(selector labels.Selector) ([]*storagev1.StorageClass, error) {
	var matchingSCs []*storagev1.StorageClass
	for _, storageClass := range f.scs {
		// Check if the StorageClass's labels match the provided selector.
		if selector.Matches(labels.Set(storageClass.Labels)) {
			matchingSCs = append(matchingSCs, storageClass.DeepCopy())
		}
	}
	return matchingSCs, nil
}
