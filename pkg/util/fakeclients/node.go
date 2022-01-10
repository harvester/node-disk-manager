package fakeclients

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"

	lhtype "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/typed/longhorn.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
)

type NodeClient func(string) lhtype.NodeInterface

func (c NodeClient) Update(node *longhornv1.Node) (*longhornv1.Node, error) {
	return c(node.Namespace).Update(context.TODO(), node, metav1.UpdateOptions{})
}
func (c NodeClient) Get(namespace, name string, options metav1.GetOptions) (*longhornv1.Node, error) {
	panic("implement me")
}
func (c NodeClient) Create(*longhornv1.Node) (*longhornv1.Node, error) {
	panic("implement me")
}
func (c NodeClient) Delete(namespace, name string, options *metav1.DeleteOptions) error {
	panic("implement me")
}
func (c NodeClient) List(namespace string, opts metav1.ListOptions) (*longhornv1.NodeList, error) {
	panic("implement me")
}
func (c NodeClient) UpdateStatus(*longhornv1.Node) (*longhornv1.Node, error) {
	panic("implement me")
}
func (c NodeClient) Watch(namespace string, opts metav1.ListOptions) (watch.Interface, error) {
	panic("implement me")
}
func (c NodeClient) Patch(namespace, name string, pt types.PatchType, data []byte, subresources ...string) (result *longhornv1.Node, err error) {
	panic("implement me")
}

type NodeCache func(string) lhtype.NodeInterface

func (c NodeCache) Get(namespace, name string) (*longhornv1.Node, error) {
	return c(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}
func (c NodeCache) List(namespace string, selector labels.Selector) ([]*longhornv1.Node, error) {
	list, err := c(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, err
	}
	result := make([]*longhornv1.Node, 0, len(list.Items))
	for i := range list.Items {
		result = append(result, &list.Items[i])
	}
	return result, err
}

func (c NodeCache) AddIndexer(indexName string, indexer ctllonghornv1.NodeIndexer) {
	panic("implement me")
}
func (c NodeCache) GetByIndex(indexName, key string) ([]*longhornv1.Node, error) {
	panic("implement me")
}
