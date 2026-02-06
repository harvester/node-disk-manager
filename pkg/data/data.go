package data

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Init adds built-in resources
func Init(restConfig *rest.Config) error {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	if err := addNDMConfigMap(clientset); err != nil {
		return err
	}

	return nil
}
