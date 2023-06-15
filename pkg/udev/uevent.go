package udev

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/pilebones/go-udev/netlink"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/controller/blockdevice"
	"github.com/harvester/node-disk-manager/pkg/option"
	"github.com/harvester/node-disk-manager/pkg/utils"
)

type Udev struct {
	namespace string
	nodeName  string
	startOnce sync.Once
	scanner   *blockdevice.Scanner
}

func NewUdev(opt *option.Option, scanner *blockdevice.Scanner) *Udev {
	return &Udev{
		startOnce: sync.Once{},
		namespace: opt.Namespace,
		nodeName:  opt.NodeName,
		scanner:   scanner,
	}
}

func (u *Udev) Monitor(ctx context.Context) {
	u.startOnce.Do(func() {
		u.monitor(ctx)
	})
}

func (u *Udev) monitor(ctx context.Context) {
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
	errors := make(chan error)
	quit := conn.Monitor(uqueue, errors, matcher)

	// Handling message from udev queue
	for {
		select {
		case uevent := <-uqueue:
			u.ActionHandler(uevent)
		case err := <-errors:
			logrus.Errorf("failed to parse udev event, error: %s", err.Error())
		case <-ctx.Done():
			close(quit)
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
			autoProvisioned := udevDevice.IsDisk() && u.scanner.ApplyAutoProvisionFiltersForDisk(disk)
			u.AddBlockDevice(bd, autoProvisioned)
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

// AddBlockDevice add new block device and partitions by watching the udev add action
func (u *Udev) AddBlockDevice(device *v1beta1.BlockDevice, autoProvisioned bool) {
	_, err := u.scanner.SaveBlockDevice(device, autoProvisioned)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		logrus.Errorf("failed to save block device %s, error: %s", device.Name, err.Error())
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
