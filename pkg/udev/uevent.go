package udev

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/pilebones/go-udev/netlink"
	"github.com/sirupsen/logrus"

	"github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/controller/blockdevice"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type Udev struct {
	namespace   string
	nodeName    string
	startOnce   sync.Once
	scanner     *blockdevice.Scanner
	injectError bool
}

func NewUdev(opt *option.Option, scanner *blockdevice.Scanner) *Udev {
	return &Udev{
		startOnce:   sync.Once{},
		namespace:   opt.Namespace,
		nodeName:    opt.NodeName,
		scanner:     scanner,
		injectError: opt.InjectUdevMonitorError,
	}
}

func (u *Udev) Monitor(ctx context.Context) {
	// we need to respawn the monitor with any error.
	// because any error will break the monitor loop.
	errChan := make(chan error)
	go u.spawnMonitor(ctx, errChan)

}

func (u *Udev) spawnMonitor(ctx context.Context, errChan chan error) {
	go u.monitor(ctx, errChan)
	for {
		select {
		case err := <-errChan:
			logrus.Errorf("failed to monitor udev events, error: %s", err.Error())
			go u.monitor(ctx, errChan)
		case <-ctx.Done():
			return
		}
	}
}

func (u *Udev) monitor(ctx context.Context, errors chan error) {
	logrus.Infoln("Start monitoring udev processed events")

	matcher, err := getOptionalMatcher(nil)
	if err != nil {
		logrus.Fatalf("Failed to get udev config, error: %s", err.Error())
	}

	conn := new(netlink.UEventConn)
	if err := conn.Connect(netlink.UdevEvent); err != nil {
		logrus.Fatalf("Unable to connect to Netlink Kobject UEvent socket, error: %s", err.Error())
	}
	defer conn.Close()

	uqueue := make(chan netlink.UEvent)
	errChan := make(chan error)
	quit := conn.Monitor(uqueue, errChan, matcher)
	defer close(quit)

	// simulator the error from udev monitor
	if u.injectError {
		logrus.Infof("Injecting error to udev monitor for testing")
		errors <- fmt.Errorf("testing error")
		u.injectError = false
		return
	}
	// Handling message from udev queue
	for {
		select {
		case uevent := <-uqueue:
			u.ActionHandler(uevent)
		case err := <-errChan:
			errors <- err
			return
		case <-ctx.Done():
			return
		}
	}
}

func (u *Udev) ActionHandler(uevent netlink.UEvent) {
	udevDevice := InitUdevDevice(uevent.Env)
	if !udevDevice.IsDisk() && !udevDevice.IsPartition() {
		return
	}
	logrus.Debugf("Prepare to handle event: %s, env: %+v", uevent.Action, uevent.Env)

	var disk *block.Disk
	var part *block.Partition
	var bd *v1beta1.BlockDevice
	devPath := udevDevice.GetDevName()
	if udevDevice.IsDisk() {
		disk = u.scanner.BlockInfo.GetDiskByDevPath(devPath)
		bd = blockdevice.GetDiskBlockDevice(disk, u.nodeName, u.namespace)
	} else {
		parentPath, err := block.GetParentDevName(devPath)
		if err != nil {
			logrus.Errorf("failed to get parent dev name, %s", err.Error())
		}
		part = u.scanner.BlockInfo.GetPartitionByDevPath(parentPath, devPath)
		disk = part.Disk
		bd = blockdevice.GetPartitionBlockDevice(part, u.nodeName, u.namespace)
	}

	if u.scanner.ApplyExcludeFiltersForDisk(disk) {
		return
	}

	if part != nil && u.scanner.ApplyExcludeFiltersForPartition(part) {
		return
	}

	switch uevent.Action {
	case netlink.ADD:
		if bd.Name == "" {
			logrus.Infof("Skip adding non-identifiable block device %s", bd.Spec.DevPath)
			return
		}
		utils.CallerWithCondLock(u.scanner.Cond, func() any {
			// just wake up scanner to check if the disk is added, do no-op internally
			logrus.Infof("Wake up scanner with %s operation with blockdevice: %s", netlink.ADD, bd.Name)
			u.scanner.Cond.Signal()
			return nil
		})
	case netlink.REMOVE:
		if udevDevice.IsDisk() {
			// just wake up scanner to check if the disk is removed, do no-op internally
			utils.CallerWithCondLock(u.scanner.Cond, func() any {
				logrus.Infof("Wake up scanner with %s operation with blockdevice: %s", netlink.REMOVE, bd.Name)
				u.scanner.Cond.Signal()
				return nil
			})
		}
	}
}

// getOptionalMatcher Parse and load config file which contains rules for matching
func getOptionalMatcher(filePath *string) (matcher netlink.Matcher, err error) {
	if filePath == nil || *filePath == "" {
		return nil, nil
	}

	stream, err := os.ReadFile(*filePath)
	if err != nil {
		return nil, err
	}

	if stream == nil {
		return nil, fmt.Errorf("empty, no rules provided in \"%s\", err: %w", *filePath, err)
	}

	var rules netlink.RuleDefinitions
	if err := json.Unmarshal(stream, &rules); err != nil {
		return nil, fmt.Errorf("wrong rule syntax, err: %w", err)
	}

	return &rules, nil
}
