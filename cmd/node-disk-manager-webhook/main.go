package main

import (
	"context"
	"fmt"
	"os"

	"github.com/harvester/webhook/pkg/config"
	"github.com/harvester/webhook/pkg/server"
	"github.com/harvester/webhook/pkg/server/admission"
	"github.com/rancher/wrangler/v3/pkg/kubeconfig"
	"github.com/rancher/wrangler/v3/pkg/signals"
	"github.com/rancher/wrangler/v3/pkg/start"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"k8s.io/client-go/rest"

	ctldisk "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/webhook/blockdevice"
)

const webhookName = "harvester-node-disk-manager-webhook"

type resourceCaches struct {
	bdCache ctldiskv1.BlockDeviceCache
}

func main() {
	var options config.Options
	var logLevel string

	flags := []cli.Flag{
		&cli.StringFlag{
			Name:        "loglevel",
			Usage:       "Specify log level",
			EnvVars:     []string{"LOGLEVEL"},
			Value:       "info",
			Destination: &logLevel,
		},
		&cli.IntFlag{
			Name:        "threadiness",
			EnvVars:     []string{"THREADINESS"},
			Usage:       "Specify controller threads",
			Value:       5,
			Destination: &options.Threadiness,
		},
		&cli.IntFlag{
			Name:        "https-port",
			EnvVars:     []string{"WEBHOOK_SERVER_HTTPS_PORT"},
			Usage:       "HTTPS listen port",
			Value:       8443,
			Destination: &options.HTTPSListenPort,
		},
		&cli.StringFlag{
			Name:        "namespace",
			EnvVars:     []string{"NAMESPACE"},
			Destination: &options.Namespace,
			Usage:       "The harvester namespace",
			Value:       "harvester-system",
			Required:    true,
		},
		&cli.StringFlag{
			Name:        "controller-user",
			EnvVars:     []string{"CONTROLLER_USER_NAME"},
			Destination: &options.ControllerUsername,
			Value:       "system:serviceaccount:harvester-system:harvester-node-disk-manager",
			Usage:       "The harvester-node-disk-manager controller username",
		},
		&cli.StringFlag{
			Name:        "gc-user",
			EnvVars:     []string{"GARBAGE_COLLECTION_USER_NAME"},
			Destination: &options.GarbageCollectionUsername,
			Usage:       "The system username that performs garbage collection",
			Value:       "system:serviceaccount:kube-system:generic-garbage-collector",
		},
	}

	cfg, err := kubeconfig.GetNonInteractiveClientConfig(os.Getenv("KUBECONFIG")).ClientConfig()
	if err != nil {
		logrus.Fatal(err)
	}

	ctx := signals.SetupSignalContext()

	app := cli.NewApp()
	app.Flags = flags
	app.Action = func(_ *cli.Context) error {
		setLogLevel(logLevel)
		err := runWebhookServer(ctx, cfg, &options)
		return err
	}

	if err := app.Run(os.Args); err != nil {
		logrus.Fatalf("run webhook server failed: %v", err)
	}
}

func runWebhookServer(ctx context.Context, cfg *rest.Config, options *config.Options) error {
	resourceCaches, err := newCaches(ctx, cfg, options.Threadiness)
	if err != nil {
		return fmt.Errorf("error building resource caches: %s", err.Error())
	}

	webhookServer := server.NewWebhookServer(ctx, cfg, webhookName, options)

	bdMutator := blockdevice.NewBlockdeviceMutator(resourceCaches.bdCache)
	var mutators = []admission.Mutator{
		bdMutator,
	}

	bdValidator := blockdevice.NewBlockdeviceValidator(resourceCaches.bdCache)
	var validators = []admission.Validator{
		bdValidator,
	}

	if err := webhookServer.RegisterMutators(mutators...); err != nil {
		return fmt.Errorf("failed to register mutators: %v", err)
	}

	if err := webhookServer.RegisterValidators(validators...); err != nil {
		return fmt.Errorf("failed to register validators: %v", err)
	}

	if err := webhookServer.Start(); err != nil {
		return fmt.Errorf("failed to start webhook server: %v", err)
	}

	<-ctx.Done()
	return nil

}

func newCaches(ctx context.Context, cfg *rest.Config, threadiness int) (*resourceCaches, error) {
	var starters []start.Starter

	disks, err := ctldisk.NewFactoryFromConfig(cfg)
	if err != nil {
		return nil, err
	}
	starters = append(starters, disks)
	resourceCaches := &resourceCaches{
		bdCache: disks.Harvesterhci().V1beta1().BlockDevice().Cache(),
	}

	if err := start.All(ctx, threadiness, starters...); err != nil {
		return nil, err
	}

	return resourceCaches, nil
}

func setLogLevel(level string) {
	ll, err := logrus.ParseLevel(level)
	if err != nil {
		ll = logrus.DebugLevel
	}
	// set global log level
	logrus.SetLevel(ll)
}
