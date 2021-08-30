package fakeclients

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	harvesterv1beta1type "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/typed/harvesterhci.io/v1beta1"
	ctlharvesterv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
)

type BlockeDeviceClient func(string) harvesterv1beta1type.BlockDeviceInterface

func (c BlockeDeviceClient) Create(bd *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	return c(bd.Namespace).Create(context.TODO(), bd, metav1.CreateOptions{})
}

func (c BlockeDeviceClient) Update(bd *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	return c(bd.Namespace).Update(context.TODO(), bd, metav1.UpdateOptions{})
}

func (c BlockeDeviceClient) UpdateStatus(bd *diskv1.BlockDevice) (*diskv1.BlockDevice, error) {
	panic("implement me")
}

func (c BlockeDeviceClient) Delete(namespace, name string, options *metav1.DeleteOptions) error {
	return c(namespace).Delete(context.TODO(), name, *options)
}

func (c BlockeDeviceClient) Get(namespace, name string, options metav1.GetOptions) (*diskv1.BlockDevice, error) {
	return c(namespace).Get(context.TODO(), name, options)
}

func (c BlockeDeviceClient) List(namespace string, opts metav1.ListOptions) (*diskv1.BlockDeviceList, error) {
	return c(namespace).List(context.TODO(), opts)
}

func (c BlockeDeviceClient) Watch(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return c(namespace).Watch(context.TODO(), opts)
}

func (c BlockeDeviceClient) Patch(namespace, name string, pt types.PatchType, data []byte, subresources ...string) (*diskv1.BlockDevice, error) {
	return c(namespace).Patch(context.TODO(), name, pt, data, metav1.PatchOptions{}, subresources...)
}

type BlockeDeviceCache func(string) harvesterv1beta1type.BlockDeviceInterface

func (c BlockeDeviceCache) Get(namespace, name string) (*diskv1.BlockDevice, error) {
	return c(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (c BlockeDeviceCache) List(namespace string, selector labels.Selector) ([]*diskv1.BlockDevice, error) {
	list, err := c(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}
	result := make([]*diskv1.BlockDevice, 0, len(list.Items))
	for _, bd := range list.Items {
		result = append(result, &bd)
	}
	return result, err
}

func (c BlockeDeviceCache) AddIndexer(indexName string, indexer ctlharvesterv1.BlockDeviceIndexer) {
	panic("implement me")
}

func (c BlockeDeviceCache) GetByIndex(indexName, key string) ([]*diskv1.BlockDevice, error) {
	panic("implement me")
}
