package main

import (
	"os"

	longhornv1 "github.com/longhorn/longhorn-manager/k8s/pkg/apis/longhorn/v1beta2"
	controllergen "github.com/rancher/wrangler/v2/pkg/controller-gen"
	"github.com/rancher/wrangler/v2/pkg/controller-gen/args"

	diskv1 "github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
)

func main() {
	os.Unsetenv("GOPATH")
	controllergen.Run(args.Options{
		OutputPackage: "github.com/harvester/node-disk-manager/pkg/generated",
		Boilerplate:   "scripts/boilerplate.go.txt",
		Groups: map[string]args.Group{
			"harvesterhci.io": {
				Types: []interface{}{
					diskv1.BlockDevice{},
				},
				GenerateTypes:   true,
				GenerateClients: true,
			},
			longhornv1.SchemeGroupVersion.Group: {
				Types: []interface{}{
					longhornv1.Node{},
				},
				GenerateTypes:   false,
				GenerateClients: true,
			},
		},
	})
}
