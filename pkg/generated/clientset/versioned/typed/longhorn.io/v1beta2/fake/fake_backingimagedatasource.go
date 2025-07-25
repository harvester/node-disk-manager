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

package fake

import (
	longhorniov1beta2 "github.com/harvester/node-disk-manager/pkg/generated/clientset/versioned/typed/longhorn.io/v1beta2"
	v1beta2 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	gentype "k8s.io/client-go/gentype"
)

// fakeBackingImageDataSources implements BackingImageDataSourceInterface
type fakeBackingImageDataSources struct {
	*gentype.FakeClientWithList[*v1beta2.BackingImageDataSource, *v1beta2.BackingImageDataSourceList]
	Fake *FakeLonghornV1beta2
}

func newFakeBackingImageDataSources(fake *FakeLonghornV1beta2, namespace string) longhorniov1beta2.BackingImageDataSourceInterface {
	return &fakeBackingImageDataSources{
		gentype.NewFakeClientWithList[*v1beta2.BackingImageDataSource, *v1beta2.BackingImageDataSourceList](
			fake.Fake,
			namespace,
			v1beta2.SchemeGroupVersion.WithResource("backingimagedatasources"),
			v1beta2.SchemeGroupVersion.WithKind("BackingImageDataSource"),
			func() *v1beta2.BackingImageDataSource { return &v1beta2.BackingImageDataSource{} },
			func() *v1beta2.BackingImageDataSourceList { return &v1beta2.BackingImageDataSourceList{} },
			func(dst, src *v1beta2.BackingImageDataSourceList) { dst.ListMeta = src.ListMeta },
			func(list *v1beta2.BackingImageDataSourceList) []*v1beta2.BackingImageDataSource {
				return gentype.ToPointerSlice(list.Items)
			},
			func(list *v1beta2.BackingImageDataSourceList, items []*v1beta2.BackingImageDataSource) {
				list.Items = gentype.FromPointerSlice(items)
			},
		),
		fake,
	}
}
