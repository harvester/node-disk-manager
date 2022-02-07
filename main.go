//go:generate go run pkg/codegen/cleanup/main.go
//go:generate /bin/rm -rf pkg/generated
//go:generate go run pkg/codegen/main.go
//go:generate /bin/bash scripts/generate-manifest

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/harvester/node-disk-manager/pkg/filter"

	"github.com/ehazlett/simplelog"
	"github.com/rancher/wrangler/pkg/kubeconfig"
	"github.com/rancher/wrangler/pkg/signals"
	"github.com/rancher/wrangler/pkg/start"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"github.com/harvester/node-disk-manager/pkg/block"
	blockdevicev1 "github.com/harvester/node-disk-manager/pkg/controller/blockdevice"
	nodev1 "github.com/harvester/node-disk-manager/pkg/controller/node"
	ctldisk "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io"
	ctllonghorn "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/udev"
	"github.com/harvester/node-disk-manager/pkg/version"
)

func main() {
	var opt option.Option
	app := cli.NewApp()
	app.Name = "node-disk-manager"
	app.Version = version.FriendlyVersion()
	app.Usage = "node-disk-manager help to manage node disks, implementing block device partition and file system formatting."
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:        "kubeconfig",
			EnvVars:     []string{"KUBECONFIG"},
			Destination: &opt.KubeConfig,
			Usage:       "Kube config for accessing k8s cluster",
		},
		&cli.StringFlag{
			Name:        "namespace",
			DefaultText: "longhorn-system",
			EnvVars:     []string{"LONGHORN_NAMESPACE"},
			Destination: &opt.Namespace,
		},
		&cli.IntFlag{
			Name:        "threadiness",
			DefaultText: "2",
			Destination: &opt.Threadiness,
		},
		&cli.BoolFlag{
			Name:        "debug",
			EnvVars:     []string{"NDM_DEBUG"},
			Usage:       "enable debug logs",
			Destination: &opt.Debug,
		},
		&cli.StringFlag{
			Name:        "profile-listen-address",
			Value:       "0.0.0.0:6060",
			Usage:       "Address to listen on for profiling",
			Destination: &opt.ProfilerAddress,
		},
		&cli.BoolFlag{
			Name:        "trace",
			EnvVars:     []string{"NDM_TRACE"},
			Usage:       "Enable trace logs",
			Destination: &opt.Trace,
		},
		&cli.StringFlag{
			Name:        "log-format",
			EnvVars:     []string{"NDM_LOG_FORMAT"},
			Usage:       "Log format",
			Value:       "text",
			Destination: &opt.LogFormat,
		},
		&cli.StringFlag{
			Name:        "node-name",
			EnvVars:     []string{"NODE_NAME"},
			Usage:       "Specify the node name",
			Destination: &opt.NodeName,
		},
		&cli.StringFlag{
			Name:        "vendor-filter",
			Value:       "longhorn",
			EnvVars:     []string{"NDM_VENDOR_FILTER"},
			Usage:       "A string of comma-separated values that you want to exclude for block device vendor filter",
			Destination: &opt.VendorFilter,
		},
		&cli.StringFlag{
			Name:        "path-filter",
			EnvVars:     []string{"NDM_PATH_FILTER"},
			Usage:       "A string of comma-separated values that you want to exclude for block device path filter",
			Destination: &opt.PathFilter,
		},
		&cli.StringFlag{
			Name:        "label-filter",
			EnvVars:     []string{"NDM_LABEL_FILTER"},
			Usage:       "A string of comma-separated glob patterns that you want to exclude for block device filesystem label filter",
			Destination: &opt.LabelFilter,
		},
		&cli.Int64Flag{
			Name:        "rescan-interval",
			EnvVars:     []string{"NDM_RESCAN_INTERVAL"},
			Usage:       "Specify the interval of device rescanning of the node (in seconds)",
			Destination: &opt.RescanInterval,
		},
		&cli.StringFlag{
			Name:        "auto-provision-filter",
			EnvVars:     []string{"NDM_AUTO_PROVISION_FILTER"},
			Usage:       "A string of comma-separated glob patterns that auto-provisions devices matching provided device path",
			Destination: &opt.AutoProvisionFilter,
		},
	}

	app.Action = func(c *cli.Context) error {
		initProfiling(&opt)
		initLogs(&opt)
		return run(&opt)
	}

	if err := app.Run(os.Args); err != nil {
		logrus.Fatal(err)
	}
}

func initProfiling(opt *option.Option) {
	// enable profiler
	if opt.ProfilerAddress != "" {
		go func() {
			log.Println(http.ListenAndServe(opt.ProfilerAddress, nil))
		}()
	}
}

func initLogs(opt *option.Option) {
	switch opt.LogFormat {
	case "simple":
		logrus.SetFormatter(&simplelog.StandardFormatter{})
	case "json":
		logrus.SetFormatter(&logrus.JSONFormatter{})
	default:
		logrus.SetFormatter(&logrus.TextFormatter{})
	}
	logrus.SetOutput(os.Stdout)
	logrus.Infof("Node Disk Manager %s is starting", version.FriendlyVersion())
	if opt.Debug {
		logrus.SetLevel(logrus.DebugLevel)
		logrus.Debugf("Loglevel set to [%v]", logrus.DebugLevel)
	}
	if opt.Trace {
		logrus.SetLevel(logrus.TraceLevel)
		logrus.Tracef("Loglevel set to [%v]", logrus.TraceLevel)
	}
}

func run(opt *option.Option) error {
	logrus.Info("Starting node disk manager controller")
	if opt.NodeName == "" || opt.Namespace == "" {
		return errors.New("either node name or namespace is empty")
	}

	ctx := signals.SetupSignalHandler(context.Background())

	// register block device detector
	block, err := block.New()
	if err != nil {
		return err
	}

	kubeConfig, err := kubeconfig.GetNonInteractiveClientConfig(opt.KubeConfig).ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to find kubeconfig: %v", err)
	}

	disks, err := ctldisk.NewFactoryFromConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("error building node-disk-manager controllers: %s", err.Error())
	}

	lhs, err := ctllonghorn.NewFactoryFromConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("error building node-disk-manager controllers: %s", err.Error())
	}

	excludeFilters := filter.SetExcludeFilters(opt.VendorFilter, opt.PathFilter, opt.LabelFilter)
	autoProvisionFilters := filter.SetAutoProvisionFilters(opt.AutoProvisionFilter)

	start := func(ctx context.Context) {
		if err := blockdevicev1.Register(
			ctx, lhs.Longhorn().V1beta1().Node(),
			disks.Harvesterhci().V1beta1().BlockDevice(),
			block,
			opt,
			excludeFilters,
			autoProvisionFilters,
		); err != nil {
			logrus.Fatalf("failed to register block device controller, %s", err.Error())
		}

		if err := nodev1.Register(
			ctx,
			lhs.Longhorn().V1beta1().Node(),
			disks.Harvesterhci().V1beta1().BlockDevice(),
			opt,
		); err != nil {
			logrus.Fatalf("failed to register ndm node controller, %s", err.Error())
		}

		if err := start.All(ctx, opt.Threadiness, disks, lhs); err != nil {
			logrus.Fatalf("error starting, %s", err.Error())
		}

		// TODO
		// 1. support for filtering out disks from adding as custom resources
		// 2. add node actions, i.e. block device rescan

		// register to monitor the UDEV events, similar to run `udevadm monitor -u`
		go udev.NewUdev(
			lhs.Longhorn().V1beta1().Node(),
			disks.Harvesterhci().V1beta1().BlockDevice(),
			block,
			opt,
			excludeFilters,
			autoProvisionFilters,
		).Monitor(ctx)
	}

	start(ctx)

	<-ctx.Done()
	return nil
}
