/*
Copyright 2023 Rancher Labs, Inc.

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

package fake

import (
	"context"

	v1beta1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeBackingImages implements BackingImageInterface
type FakeBackingImages struct {
	Fake *FakeLonghornV1beta1
	ns   string
}

var backingimagesResource = schema.GroupVersionResource{Group: "longhorn.io", Version: "v1beta1", Resource: "backingimages"}

var backingimagesKind = schema.GroupVersionKind{Group: "longhorn.io", Version: "v1beta1", Kind: "BackingImage"}

// Get takes name of the backingImage, and returns the corresponding backingImage object, and an error if there is any.
func (c *FakeBackingImages) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1beta1.BackingImage, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(backingimagesResource, c.ns, name), &v1beta1.BackingImage{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.BackingImage), err
}

// List takes label and field selectors, and returns the list of BackingImages that match those selectors.
func (c *FakeBackingImages) List(ctx context.Context, opts v1.ListOptions) (result *v1beta1.BackingImageList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(backingimagesResource, backingimagesKind, c.ns, opts), &v1beta1.BackingImageList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1beta1.BackingImageList{ListMeta: obj.(*v1beta1.BackingImageList).ListMeta}
	for _, item := range obj.(*v1beta1.BackingImageList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested backingImages.
func (c *FakeBackingImages) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(backingimagesResource, c.ns, opts))

}

// Create takes the representation of a backingImage and creates it.  Returns the server's representation of the backingImage, and an error, if there is any.
func (c *FakeBackingImages) Create(ctx context.Context, backingImage *v1beta1.BackingImage, opts v1.CreateOptions) (result *v1beta1.BackingImage, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(backingimagesResource, c.ns, backingImage), &v1beta1.BackingImage{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.BackingImage), err
}

// Update takes the representation of a backingImage and updates it. Returns the server's representation of the backingImage, and an error, if there is any.
func (c *FakeBackingImages) Update(ctx context.Context, backingImage *v1beta1.BackingImage, opts v1.UpdateOptions) (result *v1beta1.BackingImage, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(backingimagesResource, c.ns, backingImage), &v1beta1.BackingImage{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.BackingImage), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeBackingImages) UpdateStatus(ctx context.Context, backingImage *v1beta1.BackingImage, opts v1.UpdateOptions) (*v1beta1.BackingImage, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(backingimagesResource, "status", c.ns, backingImage), &v1beta1.BackingImage{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.BackingImage), err
}

// Delete takes name of the backingImage and deletes it. Returns an error if one occurs.
func (c *FakeBackingImages) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(backingimagesResource, c.ns, name), &v1beta1.BackingImage{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeBackingImages) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(backingimagesResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1beta1.BackingImageList{})
	return err
}

// Patch applies the patch and returns the patched backingImage.
func (c *FakeBackingImages) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta1.BackingImage, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(backingimagesResource, c.ns, name, pt, data, subresources...), &v1beta1.BackingImage{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta1.BackingImage), err
}
