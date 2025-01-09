/*
Copyright 2025 Rancher Labs, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by main. DO NOT EDIT.

package v1beta2

import (
	"context"
	"time"

	scheme "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/scheme"
	v1beta2 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// SupportBundlesGetter has a method to return a SupportBundleInterface.
// A group's client should implement this interface.
type SupportBundlesGetter interface {
	SupportBundles(namespace string) SupportBundleInterface
}

// SupportBundleInterface has methods to work with SupportBundle resources.
type SupportBundleInterface interface {
	Create(ctx context.Context, supportBundle *v1beta2.SupportBundle, opts v1.CreateOptions) (*v1beta2.SupportBundle, error)
	Update(ctx context.Context, supportBundle *v1beta2.SupportBundle, opts v1.UpdateOptions) (*v1beta2.SupportBundle, error)
	UpdateStatus(ctx context.Context, supportBundle *v1beta2.SupportBundle, opts v1.UpdateOptions) (*v1beta2.SupportBundle, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1beta2.SupportBundle, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1beta2.SupportBundleList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta2.SupportBundle, err error)
	SupportBundleExpansion
}

// supportBundles implements SupportBundleInterface
type supportBundles struct {
	client rest.Interface
	ns     string
}

// newSupportBundles returns a SupportBundles
func newSupportBundles(c *LonghornV1beta2Client, namespace string) *supportBundles {
	return &supportBundles{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the supportBundle, and returns the corresponding supportBundle object, and an error if there is any.
func (c *supportBundles) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1beta2.SupportBundle, err error) {
	result = &v1beta2.SupportBundle{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("supportbundles").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of SupportBundles that match those selectors.
func (c *supportBundles) List(ctx context.Context, opts v1.ListOptions) (result *v1beta2.SupportBundleList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1beta2.SupportBundleList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("supportbundles").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested supportBundles.
func (c *supportBundles) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("supportbundles").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a supportBundle and creates it.  Returns the server's representation of the supportBundle, and an error, if there is any.
func (c *supportBundles) Create(ctx context.Context, supportBundle *v1beta2.SupportBundle, opts v1.CreateOptions) (result *v1beta2.SupportBundle, err error) {
	result = &v1beta2.SupportBundle{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("supportbundles").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(supportBundle).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a supportBundle and updates it. Returns the server's representation of the supportBundle, and an error, if there is any.
func (c *supportBundles) Update(ctx context.Context, supportBundle *v1beta2.SupportBundle, opts v1.UpdateOptions) (result *v1beta2.SupportBundle, err error) {
	result = &v1beta2.SupportBundle{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("supportbundles").
		Name(supportBundle.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(supportBundle).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *supportBundles) UpdateStatus(ctx context.Context, supportBundle *v1beta2.SupportBundle, opts v1.UpdateOptions) (result *v1beta2.SupportBundle, err error) {
	result = &v1beta2.SupportBundle{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("supportbundles").
		Name(supportBundle.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(supportBundle).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the supportBundle and deletes it. Returns an error if one occurs.
func (c *supportBundles) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("supportbundles").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *supportBundles) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("supportbundles").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched supportBundle.
func (c *supportBundles) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta2.SupportBundle, err error) {
	result = &v1beta2.SupportBundle{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("supportbundles").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
