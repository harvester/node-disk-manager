package fake

import (
	harvv1beta1 "github.com/harvester/harvester/pkg/apis/harvesterhci.io/v1beta1"
	ctlharvv1beta1 "github.com/harvester/harvester/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/rancher/wrangler/v3/pkg/generic"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FakeVMImageCache struct {
	vmImages []*harvv1beta1.VirtualMachineImage
}

func NewVMImageCache(vmImagesToServe []*harvv1beta1.VirtualMachineImage) ctlharvv1beta1.VirtualMachineImageCache {
	return &FakeVMImageCache{
		vmImages: vmImagesToServe,
	}
}

func (f *FakeVMImageCache) AddIndexer(indexName string, indexer generic.Indexer[*harvv1beta1.VirtualMachineImage]) {
	panic("unimplemented")
}

func (f *FakeVMImageCache) Get(namespace string, name string) (*harvv1beta1.VirtualMachineImage, error) {
	for _, img := range f.vmImages {
		if img.Namespace == namespace && img.Name == name {
			return img.DeepCopy(), nil
		}
	}

	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

func (f *FakeVMImageCache) GetByIndex(indexName string, key string) ([]*harvv1beta1.VirtualMachineImage, error) {
	panic("unimplemented")
}

func (f *FakeVMImageCache) List(namespace string, selector labels.Selector) ([]*harvv1beta1.VirtualMachineImage, error) {
	var matchingImages []*harvv1beta1.VirtualMachineImage

	for _, img := range f.vmImages {
		if namespace != "" && img.Namespace != namespace {
			continue
		}
		if selector.Matches(labels.Set(img.Labels)) {
			matchingImages = append(matchingImages, img.DeepCopy())
		}
	}

	return matchingImages, nil
}
