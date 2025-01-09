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

// EngineImagesGetter has a method to return a EngineImageInterface.
// A group's client should implement this interface.
type EngineImagesGetter interface {
	EngineImages(namespace string) EngineImageInterface
}

// EngineImageInterface has methods to work with EngineImage resources.
type EngineImageInterface interface {
	Create(ctx context.Context, engineImage *v1beta2.EngineImage, opts v1.CreateOptions) (*v1beta2.EngineImage, error)
	Update(ctx context.Context, engineImage *v1beta2.EngineImage, opts v1.UpdateOptions) (*v1beta2.EngineImage, error)
	UpdateStatus(ctx context.Context, engineImage *v1beta2.EngineImage, opts v1.UpdateOptions) (*v1beta2.EngineImage, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1beta2.EngineImage, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1beta2.EngineImageList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta2.EngineImage, err error)
	EngineImageExpansion
}

// engineImages implements EngineImageInterface
type engineImages struct {
	client rest.Interface
	ns     string
}

// newEngineImages returns a EngineImages
func newEngineImages(c *LonghornV1beta2Client, namespace string) *engineImages {
	return &engineImages{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the engineImage, and returns the corresponding engineImage object, and an error if there is any.
func (c *engineImages) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1beta2.EngineImage, err error) {
	result = &v1beta2.EngineImage{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("engineimages").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of EngineImages that match those selectors.
func (c *engineImages) List(ctx context.Context, opts v1.ListOptions) (result *v1beta2.EngineImageList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1beta2.EngineImageList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("engineimages").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested engineImages.
func (c *engineImages) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("engineimages").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a engineImage and creates it.  Returns the server's representation of the engineImage, and an error, if there is any.
func (c *engineImages) Create(ctx context.Context, engineImage *v1beta2.EngineImage, opts v1.CreateOptions) (result *v1beta2.EngineImage, err error) {
	result = &v1beta2.EngineImage{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("engineimages").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(engineImage).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a engineImage and updates it. Returns the server's representation of the engineImage, and an error, if there is any.
func (c *engineImages) Update(ctx context.Context, engineImage *v1beta2.EngineImage, opts v1.UpdateOptions) (result *v1beta2.EngineImage, err error) {
	result = &v1beta2.EngineImage{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("engineimages").
		Name(engineImage.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(engineImage).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *engineImages) UpdateStatus(ctx context.Context, engineImage *v1beta2.EngineImage, opts v1.UpdateOptions) (result *v1beta2.EngineImage, err error) {
	result = &v1beta2.EngineImage{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("engineimages").
		Name(engineImage.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(engineImage).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the engineImage and deletes it. Returns an error if one occurs.
func (c *engineImages) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("engineimages").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *engineImages) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("engineimages").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched engineImage.
func (c *engineImages) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta2.EngineImage, err error) {
	result = &v1beta2.EngineImage{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("engineimages").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
