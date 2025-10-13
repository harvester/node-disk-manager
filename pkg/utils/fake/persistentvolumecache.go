package fake

import (
	ctlcorev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core/v1"
	"github.com/rancher/wrangler/v3/pkg/generic"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FakePersistentVolumeCache struct {
	pvs []*v1.PersistentVolume
}

func NewPersistentVolumeCache(pvsToServe []*v1.PersistentVolume) ctlcorev1.PersistentVolumeCache {
	return &FakePersistentVolumeCache{
		pvs: pvsToServe,
	}
}

func (f *FakePersistentVolumeCache) AddIndexer(indexName string, indexer generic.Indexer[*v1.PersistentVolume]) {
	panic("unimplemented")
}

func (f *FakePersistentVolumeCache) Get(name string) (*v1.PersistentVolume, error) {
	for _, pv := range f.pvs {
		if pv.Name == name {
			return pv.DeepCopy(), nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

func (f *FakePersistentVolumeCache) GetByIndex(indexName string, key string) ([]*v1.PersistentVolume, error) {
	panic("unimplemented")
}

func (f *FakePersistentVolumeCache) List(selector labels.Selector) ([]*v1.PersistentVolume, error) {
	var matchingPVs []*v1.PersistentVolume
	for _, pv := range f.pvs {
		if selector.Matches(labels.Set(pv.Labels)) {
			matchingPVs = append(matchingPVs, pv.DeepCopy())
		}
	}
	return matchingPVs, nil
}
