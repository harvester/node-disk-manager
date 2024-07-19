/*
Copyright 2024 Rancher Labs, Inc.

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

	v1beta2 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeShareManagers implements ShareManagerInterface
type FakeShareManagers struct {
	Fake *FakeLonghornV1beta2
	ns   string
}

var sharemanagersResource = v1beta2.SchemeGroupVersion.WithResource("sharemanagers")

var sharemanagersKind = v1beta2.SchemeGroupVersion.WithKind("ShareManager")

// Get takes name of the shareManager, and returns the corresponding shareManager object, and an error if there is any.
func (c *FakeShareManagers) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1beta2.ShareManager, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(sharemanagersResource, c.ns, name), &v1beta2.ShareManager{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta2.ShareManager), err
}

// List takes label and field selectors, and returns the list of ShareManagers that match those selectors.
func (c *FakeShareManagers) List(ctx context.Context, opts v1.ListOptions) (result *v1beta2.ShareManagerList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(sharemanagersResource, sharemanagersKind, c.ns, opts), &v1beta2.ShareManagerList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1beta2.ShareManagerList{ListMeta: obj.(*v1beta2.ShareManagerList).ListMeta}
	for _, item := range obj.(*v1beta2.ShareManagerList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested shareManagers.
func (c *FakeShareManagers) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(sharemanagersResource, c.ns, opts))

}

// Create takes the representation of a shareManager and creates it.  Returns the server's representation of the shareManager, and an error, if there is any.
func (c *FakeShareManagers) Create(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.CreateOptions) (result *v1beta2.ShareManager, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(sharemanagersResource, c.ns, shareManager), &v1beta2.ShareManager{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta2.ShareManager), err
}

// Update takes the representation of a shareManager and updates it. Returns the server's representation of the shareManager, and an error, if there is any.
func (c *FakeShareManagers) Update(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.UpdateOptions) (result *v1beta2.ShareManager, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(sharemanagersResource, c.ns, shareManager), &v1beta2.ShareManager{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta2.ShareManager), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeShareManagers) UpdateStatus(ctx context.Context, shareManager *v1beta2.ShareManager, opts v1.UpdateOptions) (*v1beta2.ShareManager, error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateSubresourceAction(sharemanagersResource, "status", c.ns, shareManager), &v1beta2.ShareManager{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta2.ShareManager), err
}

// Delete takes name of the shareManager and deletes it. Returns an error if one occurs.
func (c *FakeShareManagers) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(sharemanagersResource, c.ns, name, opts), &v1beta2.ShareManager{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeShareManagers) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(sharemanagersResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1beta2.ShareManagerList{})
	return err
}

// Patch applies the patch and returns the patched shareManager.
func (c *FakeShareManagers) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1beta2.ShareManager, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(sharemanagersResource, c.ns, name, pt, data, subresources...), &v1beta2.ShareManager{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1beta2.ShareManager), err
}
