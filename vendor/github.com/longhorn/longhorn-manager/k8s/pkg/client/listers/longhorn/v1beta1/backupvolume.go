/*
Copyright The Kubernetes Authors.

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

// Code generated by lister-gen. DO NOT EDIT.

package v1beta1

import (
	v1beta1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// BackupVolumeLister helps list BackupVolumes.
type BackupVolumeLister interface {
	// List lists all BackupVolumes in the indexer.
	List(selector labels.Selector) (ret []*v1beta1.BackupVolume, err error)
	// BackupVolumes returns an object that can list and get BackupVolumes.
	BackupVolumes(namespace string) BackupVolumeNamespaceLister
	BackupVolumeListerExpansion
}

// backupVolumeLister implements the BackupVolumeLister interface.
type backupVolumeLister struct {
	indexer cache.Indexer
}

// NewBackupVolumeLister returns a new BackupVolumeLister.
func NewBackupVolumeLister(indexer cache.Indexer) BackupVolumeLister {
	return &backupVolumeLister{indexer: indexer}
}

// List lists all BackupVolumes in the indexer.
func (s *backupVolumeLister) List(selector labels.Selector) (ret []*v1beta1.BackupVolume, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.BackupVolume))
	})
	return ret, err
}

// BackupVolumes returns an object that can list and get BackupVolumes.
func (s *backupVolumeLister) BackupVolumes(namespace string) BackupVolumeNamespaceLister {
	return backupVolumeNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// BackupVolumeNamespaceLister helps list and get BackupVolumes.
type BackupVolumeNamespaceLister interface {
	// List lists all BackupVolumes in the indexer for a given namespace.
	List(selector labels.Selector) (ret []*v1beta1.BackupVolume, err error)
	// Get retrieves the BackupVolume from the indexer for a given namespace and name.
	Get(name string) (*v1beta1.BackupVolume, error)
	BackupVolumeNamespaceListerExpansion
}

// backupVolumeNamespaceLister implements the BackupVolumeNamespaceLister
// interface.
type backupVolumeNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all BackupVolumes in the indexer for a given namespace.
func (s backupVolumeNamespaceLister) List(selector labels.Selector) (ret []*v1beta1.BackupVolume, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.BackupVolume))
	})
	return ret, err
}

// Get retrieves the BackupVolume from the indexer for a given namespace and name.
func (s backupVolumeNamespaceLister) Get(name string) (*v1beta1.BackupVolume, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1beta1.Resource("backupvolume"), name)
	}
	return obj.(*v1beta1.BackupVolume), nil
}
