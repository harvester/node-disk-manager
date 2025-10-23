package fake

import (
	lhv1beta2 "github.com/harvester/harvester/pkg/generated/controllers/longhorn.io/v1beta2"
	lhv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	"github.com/rancher/wrangler/v3/pkg/generic"
	"k8s.io/apimachinery/pkg/labels"
)

type FakeBackingImageCache struct {
	backingImages []*lhv1.BackingImage
}

func NewBackingImageCache(backingImagesToServe []*lhv1.BackingImage) lhv1beta2.BackingImageCache {
	return &FakeBackingImageCache{
		backingImages: backingImagesToServe,
	}
}

func (f *FakeBackingImageCache) AddIndexer(indexName string, indexer generic.Indexer[*lhv1.BackingImage]) {
}

func (f *FakeBackingImageCache) Get(namespace string, name string) (*lhv1.BackingImage, error) {
	panic("unimplemented")
}

func (f *FakeBackingImageCache) GetByIndex(indexName string, key string) ([]*lhv1.BackingImage, error) {
	return f.backingImages, nil
}

func (f *FakeBackingImageCache) List(namespace string, selector labels.Selector) ([]*lhv1.BackingImage, error) {
	panic("unimplemented")
}
