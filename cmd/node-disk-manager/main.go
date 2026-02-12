package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sync"
	"time"

	"github.com/ehazlett/simplelog"
	ctlharvester "github.com/harvester/harvester/pkg/generated/controllers/harvesterhci.io"
	k8scorev1 "github.com/rancher/wrangler/v3/pkg/generated/controllers/core"
	"github.com/rancher/wrangler/v3/pkg/kubeconfig"
	"github.com/rancher/wrangler/v3/pkg/signals"
	"github.com/rancher/wrangler/v3/pkg/start"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"github.com/harvester/node-disk-manager/pkg/block"
	blockdevicev1 "github.com/harvester/node-disk-manager/pkg/controller/blockdevice"
	nodev1 "github.com/harvester/node-disk-manager/pkg/controller/node"
	volumegroupv1 "github.com/harvester/node-disk-manager/pkg/controller/volumegroup"
	"github.com/harvester/node-disk-manager/pkg/data"
	"github.com/harvester/node-disk-manager/pkg/filter"
	ctldisk "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io"
	ctllonghorn "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/udev"
	"github.com/harvester/node-disk-manager/pkg/utils"
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
			Value:       "longhorn-system",
			DefaultText: "longhorn-system",
			EnvVars:     []string{"LONGHORN_NAMESPACE"},
			Destination: &opt.Namespace,
		},
		&cli.IntFlag{
			Name:        "threadiness",
			Value:       2,
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
			Value:       "",
			Usage:       "Address to listen on for profiling, e.g. `:6060`",
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
			DefaultText: "text",
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
			DefaultText: "longhorn",
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
		&cli.StringFlag{
			Name:        "auto-provision-filter",
			EnvVars:     []string{"NDM_AUTO_PROVISION_FILTER"},
			Usage:       "A string of comma-separated glob patterns that auto-provisions devices matching provided device path",
			Destination: &opt.AutoProvisionFilter,
		},
		&cli.UintFlag{
			Name:        "max-concurrent-ops",
			EnvVars:     []string{"NDM_MAX_CONCURRENT_OPS"},
			Usage:       "Specify the maximum concurrent count of disk operations, such as formatting",
			Value:       5,
			DefaultText: "5",
			Destination: &opt.MaxConcurrentOps,
		},
		&cli.BoolFlag{
			Name:        "inject-udev-monitor-error",
			EnvVars:     []string{"NDM_INJECT_UDEV_MONITOR_ERROR"},
			Usage:       "Inject error when monitoring udev events",
			Value:       false,
			Destination: &opt.InjectUdevMonitorError,
		},
	}

	app.Action = func(_ *cli.Context) error {
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
			profilerServer := &http.Server{
				Addr:              opt.ProfilerAddress,
				ReadHeaderTimeout: 10 * time.Second,
			}
			log.Println(profilerServer.ListenAndServe())
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
	logrus.Infof("Notable parameters are following:")
	logrus.Infof("Namespace: %s, ConcurrentOps: %d, InjectUdevMonitorError: %v",
		opt.Namespace, opt.MaxConcurrentOps, opt.InjectUdevMonitorError)
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

	ctx := signals.SetupSignalContext()

	// register block device detector
	block, err := block.New()
	if err != nil {
		return err
	}

	kubeConfig, err := kubeconfig.GetNonInteractiveClientConfig(opt.KubeConfig).ClientConfig()
	if err != nil {
		return fmt.Errorf("failed to find kubeconfig: %v", err)
	}

	// Initialize built-in resources (e.g., ConfigMap)
	if err := data.Init(kubeConfig); err != nil {
		return fmt.Errorf("failed to initialize built-in resources: %v", err)
	}

	harvesters, err := ctlharvester.NewFactoryFromConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("error building node-disk-manager controllers: %s", err.Error())
	}

	disks, err := ctldisk.NewFactoryFromConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("error building node-disk-manager controllers: %s", err.Error())
	}

	lhs, err := ctllonghorn.NewFactoryFromConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("error building node-disk-manager controllers: %s", err.Error())
	}

	corev1, err := k8scorev1.NewFactoryFromConfig(kubeConfig)
	if err != nil {
		return fmt.Errorf("error creating core/v1 factory access: %s", err.Error())
	}

	configmap := corev1.Core().V1().ConfigMap()

	// Create ConfigMapLoader for dynamic configuration reloading
	// The env variables are used as fallback when ConfigMap is not available or empty
	configMapLoader := filter.NewConfigMapLoader(
		configmap,
		filter.DefaultConfigMapNamespace,
		opt.NodeName,
		opt.VendorFilter,
		opt.PathFilter,
		opt.LabelFilter,
		opt.AutoProvisionFilter,
	)

	terminatedChannel := make(chan bool, 1)

	locker := &sync.Mutex{}
	cond := sync.NewCond(locker)
	upgrades := harvesters.Harvesterhci().V1beta1().Upgrade()
	bds := disks.Harvesterhci().V1beta1().BlockDevice()
	lvmVGs := disks.Harvesterhci().V1beta1().LVMVolumeGroup()
	nodes := lhs.Longhorn().V1beta2().Node()
	scanner := blockdevicev1.NewScanner(
		opt.NodeName,
		opt.Namespace,
		upgrades,
		bds,
		block,
		configMapLoader,
		cond,
		false,
		&terminatedChannel,
	)

	start := func(ctx context.Context) {
		if err := blockdevicev1.Register(
			ctx,
			nodes,
			upgrades,
			bds,
			lvmVGs,
			configmap,
			block,
			opt,
			scanner,
		); err != nil {
			logrus.Fatalf("failed to register block device controller, %s", err.Error())
		}

		if err := nodev1.Register(ctx, nodes, bds, opt); err != nil {
			logrus.Fatalf("failed to register ndm node controller, %s", err.Error())
		}

		if err := volumegroupv1.Register(ctx, lvmVGs, opt); err != nil {
			logrus.Fatalf("failed to register ndm volume group controller, %s", err.Error())
		}

		if err := start.All(ctx, opt.Threadiness, disks, lhs, corev1); err != nil {
			logrus.Fatalf("error starting, %s", err.Error())
		}

		// TODO
		// 1. support for filtering out disks from adding as custom resources
		// 2. add node actions, i.e. block device rescan

		// register to monitor the UDEV events, similar to run `udevadm monitor -u`
		go udev.NewUdev(opt, scanner).Monitor(ctx)
	}

	start(ctx)

	<-ctx.Done()
	scanner.Shutdown = true
	logrus.Infof("NDM is shutting down")
	utils.CallerWithCondLock(scanner.Cond, func() any {
		scanner.Cond.Signal()
		return nil
	})
	<-terminatedChannel
	return nil
}
