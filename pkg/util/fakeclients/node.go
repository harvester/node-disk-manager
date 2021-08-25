package fakeclients

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	lhv1beta1type "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/typed/longhorn.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
)

type NodeClient func(string) lhv1beta1type.NodeInterface

func (c NodeClient) Create(node *longhornv1.Node) (*longhornv1.Node, error) {
	return c(node.Namespace).Create(context.TODO(), node, metav1.CreateOptions{})
}

func (c NodeClient) Update(node *longhornv1.Node) (*longhornv1.Node, error) {
	return c(node.Namespace).Update(context.TODO(), node, metav1.UpdateOptions{})
}

func (c NodeClient) UpdateStatus(node *longhornv1.Node) (*longhornv1.Node, error) {
	panic("implement me")
}

func (c NodeClient) Delete(namespace, name string, options *metav1.DeleteOptions) error {
	return c(namespace).Delete(context.TODO(), name, *options)
}

func (c NodeClient) Get(namespace, name string, options metav1.GetOptions) (*longhornv1.Node, error) {
	return c(namespace).Get(context.TODO(), name, options)
}

func (c NodeClient) List(namespace string, opts metav1.ListOptions) (*longhornv1.NodeList, error) {
	return c(namespace).List(context.TODO(), opts)
}

func (c NodeClient) Watch(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	return c(namespace).Watch(context.TODO(), opts)
}

func (c NodeClient) Patch(namespace, name string, pt types.PatchType, data []byte, subresources ...string) (*longhornv1.Node, error) {
	return c(namespace).Patch(context.TODO(), name, pt, data, metav1.PatchOptions{}, subresources...)
}

type NodeCache func(string) lhv1beta1type.NodeInterface

func (c NodeCache) Get(namespace, name string) (*longhornv1.Node, error) {
	return c(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}

func (c NodeCache) List(namespace string, selector labels.Selector) ([]*longhornv1.Node, error) {
	panic("implement me")
}

func (c NodeCache) AddIndexer(indexName string, indexer ctllonghornv1.NodeIndexer) {
	panic("implement me")
}

func (c NodeCache) GetByIndex(indexName, key string) ([]*longhornv1.Node, error) {
	panic("implement me")
}
