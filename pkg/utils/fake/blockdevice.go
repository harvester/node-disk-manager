package fake

import (
	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/rancher/wrangler/v3/pkg/generic"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type FakeBlockDeviceCache struct {
	devices []*diskv1.BlockDevice
}

func NewBlockDeviceCache(devicesToServe []*diskv1.BlockDevice) ctldiskv1.BlockDeviceCache {
	return &FakeBlockDeviceCache{
		devices: devicesToServe,
	}
}

func (c *FakeBlockDeviceCache) AddIndexer(indexName string, indexer generic.Indexer[*diskv1.BlockDevice]) {
	panic("unimplemented")
}

func (c *FakeBlockDeviceCache) Get(namespace, name string) (*diskv1.BlockDevice, error) {
	for _, device := range c.devices {
		if device.Namespace == namespace && device.Name == name {
			return device.DeepCopy(), nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{}, name)
}

func (c *FakeBlockDeviceCache) GetByIndex(indexName, key string) ([]*diskv1.BlockDevice, error) {
	panic("unimplemented")
}

func (c *FakeBlockDeviceCache) List(namespace string, selector labels.Selector) ([]*diskv1.BlockDevice, error) {
	var matching []*diskv1.BlockDevice

	for _, device := range c.devices {
		if namespace != "" && device.Namespace != namespace {
			continue
		}

		if selector.Matches(labels.Set(device.GetLabels())) {
			matching = append(matching, device.DeepCopy())
		}
	}
	return matching, nil
}
