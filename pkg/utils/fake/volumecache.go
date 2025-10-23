package fake

import (
	lhv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	"github.com/rancher/wrangler/v3/pkg/generic"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	lhv1beta2 "github.com/harvester/harvester/pkg/generated/controllers/longhorn.io/v1beta2"
)

type FakeVolumeCache struct {
	volumes []*lhv1.Volume
}

func NewVolumeCache(volsToServe []*lhv1.Volume) lhv1beta2.VolumeCache {
	return &FakeVolumeCache{
		volumes: volsToServe,
	}
}

func (f *FakeVolumeCache) AddIndexer(indexName string, indexer generic.Indexer[*lhv1.Volume]) {
	panic("unimplemented")
}

func (f *FakeVolumeCache) Get(namespace string, name string) (*lhv1.Volume, error) {
	for _, volume := range f.volumes {
		if volume.Namespace == namespace && volume.Name == name {
			return volume.DeepCopy(), nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

func (f *FakeVolumeCache) GetByIndex(indexName string, key string) ([]*lhv1.Volume, error) {
	panic("unimplemented")
}

func (f *FakeVolumeCache) List(namespace string, selector labels.Selector) ([]*lhv1.Volume, error) {
	var matchingVolumes []*lhv1.Volume
	for _, volume := range f.volumes {
		if namespace == "" || volume.Namespace == namespace {
			if selector.Matches(labels.Set(volume.Labels)) {
				matchingVolumes = append(matchingVolumes, volume.DeepCopy())
			}
		}
	}
	return matchingVolumes, nil
}
