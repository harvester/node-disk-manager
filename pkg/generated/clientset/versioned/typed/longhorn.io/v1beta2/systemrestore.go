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
	context "context"

	scheme "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/scheme"
	longhornv1beta2 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	gentype "k8s.io/client-go/gentype"
)

// SystemRestoresGetter has a method to return a SystemRestoreInterface.
// A group's client should implement this interface.
type SystemRestoresGetter interface {
	SystemRestores(namespace string) SystemRestoreInterface
}

// SystemRestoreInterface has methods to work with SystemRestore resources.
type SystemRestoreInterface interface {
	Create(ctx context.Context, systemRestore *longhornv1beta2.SystemRestore, opts v1.CreateOptions) (*longhornv1beta2.SystemRestore, error)
	Update(ctx context.Context, systemRestore *longhornv1beta2.SystemRestore, opts v1.UpdateOptions) (*longhornv1beta2.SystemRestore, error)
	// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
	UpdateStatus(ctx context.Context, systemRestore *longhornv1beta2.SystemRestore, opts v1.UpdateOptions) (*longhornv1beta2.SystemRestore, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*longhornv1beta2.SystemRestore, error)
	List(ctx context.Context, opts v1.ListOptions) (*longhornv1beta2.SystemRestoreList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *longhornv1beta2.SystemRestore, err error)
	SystemRestoreExpansion
}

// systemRestores implements SystemRestoreInterface
type systemRestores struct {
	*gentype.ClientWithList[*longhornv1beta2.SystemRestore, *longhornv1beta2.SystemRestoreList]
}

// newSystemRestores returns a SystemRestores
func newSystemRestores(c *LonghornV1beta2Client, namespace string) *systemRestores {
	return &systemRestores{
		gentype.NewClientWithList[*longhornv1beta2.SystemRestore, *longhornv1beta2.SystemRestoreList](
			"systemrestores",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *longhornv1beta2.SystemRestore { return &longhornv1beta2.SystemRestore{} },
			func() *longhornv1beta2.SystemRestoreList { return &longhornv1beta2.SystemRestoreList{} },
		),
	}
}
