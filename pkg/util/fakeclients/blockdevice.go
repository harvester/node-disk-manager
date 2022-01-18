package fakeclients

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	diskv1type "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/typed/harvesterhci.io/v1beta1"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
)

type BlockDeviceClient func(string) diskv1type.BlockDeviceInterface

func (c BlockDeviceClient) Update(bd *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	return c(bd.Namespace).Update(context.TODO(), bd, metav1.UpdateOptions{})
}
func (c BlockDeviceClient) Get(namespace, name string, options metav1.GetOptions) (*diskv1.BlockDevice, error) {
	panic("implement me")
}
func (c BlockDeviceClient) Create(*diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	panic("implement me")
}
func (c BlockDeviceClient) Delete(namespace, name string, options *metav1.DeleteOptions) error {
	panic("implement me")
}
func (c BlockDeviceClient) List(namespace string, opts metav1.ListOptions) (*diskv1.BlockDeviceList, error) {
	panic("implement me")
}
func (c BlockDeviceClient) UpdateStatus(*diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	panic("implement me")
}
func (c BlockDeviceClient) Watch(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	panic("implement me")
}
func (c BlockDeviceClient) Patch(namespace, name string, pt types.PatchType, data []byte, subresources ...string) (result *diskv1.BlockDevice, err error) {
	panic("implement me")
}

type BlockDeviceCache func(string) diskv1type.BlockDeviceInterface

func (c BlockDeviceCache) Get(namespace, name string) (*diskv1.BlockDevice, error) {
	return c(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}
func (c BlockDeviceCache) List(namespace string, selector labels.Selector) ([]*diskv1.BlockDevice, error) {
	list, err := c(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	result := make([]*diskv1.BlockDevice, 0, len(list.Items))
	for i := range list.Items {
		result = append(result, &list.Items[i])
	}
	return result, err
}

func (c BlockDeviceCache) AddIndexer(indexName string, indexer ctldiskv1.BlockDeviceIndexer) {
}
func (c BlockDeviceCache) GetByIndex(indexName, key string) ([]*diskv1.BlockDevice, error) {
	return nil, nil
}
