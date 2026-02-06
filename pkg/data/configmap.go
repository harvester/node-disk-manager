package data

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	ConfigMapName      = "harvester-node-disk-manager"
	ConfigMapNamespace = "harvester-system"
)

func addNDMConfigMap(clientset *kubernetes.Clientset) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName,
			Namespace: ConfigMapNamespace,
		},
		Data: map[string]string{
			"filters.yaml": `- hostname: "*"
  excludeLabels: ["COS_*", "HARV_*"]
`,
			"autoprovision.yaml": "",
		},
	}

	_, err := clientset.CoreV1().ConfigMaps(ConfigMapNamespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	return nil
}
