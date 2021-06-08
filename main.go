//go:generate go run pkg/codegen/cleanup/main.go
//go:generate /bin/rm -rf pkg/generated
//go:generate go run pkg/codegen/main.go

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/longhorn/node-disk-manager/pkg/version"
	"github.com/rancher/wrangler/pkg/kubeconfig"
	"github.com/rancher/wrangler/pkg/signals"
	"github.com/rancher/wrangler/pkg/start"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"github.com/longhorn/node-disk-manager/pkg/blockdevice"
	ctldiskv1 "github.com/longhorn/node-disk-manager/pkg/generated/controllers/longhorn.io"
)

var (
	KubeConfig string
)

func main() {
	app := cli.NewApp()
	app.Name = "node-disk-manager"
	app.Version = version.FriendlyVersion()
	app.Usage = "node-disk-manager help to manage node disks, implementing block device partition and file system formatting."
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "kubeconfig",
			EnvVars:     []string{"KUBECONFIG"},
			Destination: &KubeConfig,
		},
	}
	app.Action = run

	if err := app.Run(os.Args); err != nil {
		logrus.Fatal(err)
	}
}

func run(c *cli.Context) error {
	flag.Parse()

	logrus.Info("Starting controller")
	ctx := signals.SetupSignalHandler(context.Background())

	kubeConfig, err := kubeconfig.GetNonInteractiveClientConfig(KubeConfig).ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to find kubeconfig: %v", err)
	}

	disks, err := ctldiskv1.NewFactoryFromConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("error building sample controllers: %s", err.Error())
	}

	blockdevice.Register(ctx, disks.Longhorn().V1beta1().BlockDevice())

	if err := start.All(ctx, 2); err != nil {
		return fmt.Errorf("error starting: %s", err.Error())
	}

	<-ctx.Done()
	return nil
}
