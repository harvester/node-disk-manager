module github.com/harvester/node-disk-manager

go 1.16

replace k8s.io/apimachinery => k8s.io/apimachinery v0.21.1

require (
	github.com/rancher/lasso v0.0.0-20210408231703-9ddd9378d08d
	github.com/rancher/wrangler v0.8.0
	github.com/sirupsen/logrus v1.8.1
	github.com/urfave/cli/v2 v2.3.0
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v0.21.1
)
