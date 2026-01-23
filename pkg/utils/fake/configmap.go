package fake

import (
	"context"

	"github.com/rancher/wrangler/v3/pkg/generic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	corev1type "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

// Fake ConfigMapClient implementation for testing
type FakeConfigMapClient func(namespace string) corev1type.ConfigMapInterface

func (c FakeConfigMapClient) Get(namespace, name string, options metav1.GetOptions) (*corev1.ConfigMap, error) {
	return c(namespace).Get(context.TODO(), name, options)
}

func (c FakeConfigMapClient) Create(configMap *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	return c(configMap.Namespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
}

func (c FakeConfigMapClient) Update(configMap *corev1.ConfigMap) (*corev1.ConfigMap, error) {
	return c(configMap.Namespace).Update(context.TODO(), configMap, metav1.UpdateOptions{})
}

func (c FakeConfigMapClient) Delete(namespace, name string, options *metav1.DeleteOptions) error {
	return c(namespace).Delete(context.TODO(), name, *options)
}

func (c FakeConfigMapClient) List(_ string, _ metav1.ListOptions) (*corev1.ConfigMapList, error) {
	panic("implement me")
}

func (c FakeConfigMapClient) UpdateStatus(*corev1.ConfigMap) (*corev1.ConfigMap, error) {
	panic("implement me")
}

func (c FakeConfigMapClient) Watch(_ string, _ metav1.ListOptions) (watch.Interface, error) {
	panic("implement me")
}

func (c FakeConfigMapClient) Patch(_, _ string, _ types.PatchType, _ []byte, _ ...string) (result *corev1.ConfigMap, err error) {
	panic("implement me")
}

func (c FakeConfigMapClient) WithImpersonation(_ rest.ImpersonationConfig) (generic.ClientInterface[*corev1.ConfigMap, *corev1.ConfigMapList], error) {
	panic("implement me")
}
