package udev

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"github.com/pilebones/go-udev/netlink"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/harvester/node-disk-manager/pkg/apis/harvesterhci.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/block"
	"github.com/harvester/node-disk-manager/pkg/controller/blockdevice"
	"github.com/harvester/node-disk-manager/pkg/filter"
	ctldiskv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/harvesterhci.io/v1beta1"
	ctllonghornv1 "github.com/harvester/node-disk-manager/pkg/generated/controllers/longhorn.io/v1beta1"
	"github.com/harvester/node-disk-manager/pkg/option"
)

const (
	defaultDuration time.Duration = 1
)

type Udev struct {
	namespace  string
	nodeName   string
	startOnce  sync.Once
	controller *blockdevice.Controller
}

func NewUdev(
	nodes ctllonghornv1.NodeController,
	bds ctldiskv1.BlockDeviceController,
	block block.Info,
	opt *option.Option,
	excludeFilters []*filter.Filter,
	autoProvisionFilters []*filter.Filter,
) *Udev {
	controller := &blockdevice.Controller{
		Namespace:            opt.Namespace,
		NodeName:             opt.NodeName,
		NodeCache:            nodes.Cache(),
		Nodes:                nodes,
		Blockdevices:         bds,
		BlockdeviceCache:     bds.Cache(),
		BlockInfo:            block,
		ExcludeFilters:       excludeFilters,
		AutoProvisionFilters: autoProvisionFilters,
	}
	return &Udev{
		startOnce:  sync.Once{},
		namespace:  opt.Namespace,
		nodeName:   opt.NodeName,
		controller: controller,
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

	var disk *block.Disk
	var part *block.Partition
	var bd *v1beta1.BlockDevice
	if udevDevice.IsDisk() {
		disk = u.controller.BlockInfo.GetDiskByDevPath(udevDevice.GetShortName())
		bd = blockdevice.GetDiskBlockDevice(disk, u.nodeName, u.namespace)
	} else {
		parentPath, err := block.GetParentDevName(udevDevice.GetDevName())
		logrus.Infof("debug: parent path %s", parentPath)
		if err != nil {
			logrus.Errorf("failed to get parent dev name, %s", err.Error())
		}
		part = u.controller.BlockInfo.GetPartitionByDevPath(parentPath, udevDevice.GetDevName())
		disk = part.Disk
		bd = blockdevice.GetPartitionBlockDevice(part, u.nodeName, u.namespace)
	}

	if u.controller.ApplyExcludeFiltersForDisk(disk) {
		return
	}

	if part != nil && u.controller.ApplyExcludeFiltersForPartition(part) {
		return
	}

	switch uevent.Action {
	case netlink.ADD:
		if len(bd.Name) == 0 {
			logrus.Infof("Skip adding non-identifiable block device %s", bd.Spec.DevPath)
			return
		}
		autoProvisioned := udevDevice.IsDisk() && u.controller.ApplyAutoProvisionFiltersForDisk(disk)
		u.AddBlockDevice(bd, defaultDuration, autoProvisioned)
	case netlink.REMOVE:
		if udevDevice.IsDisk() {
			u.RemoveBlockDevice(bd, &udevDevice, disk, defaultDuration)
		} else if udevDevice.IsPartition() {
			u.deactivateBlockDevice(bd)
		}
	}
}

// AddBlockDevice add new block device and partitions by watching the udev add action
func (u *Udev) AddBlockDevice(device *v1beta1.BlockDevice, duration time.Duration, autoProvisioned bool) {
	if duration > defaultDuration {
		time.Sleep(duration)
	}
	logrus.Debugf("uevent add block deivce %s", device.Spec.DevPath)

	if device == nil || device.Name == "" {
		logrus.Infof("Skip adding non-identifiable block device %s", device.Spec.DevPath)
		return
	}

	bdList, err := u.controller.BlockdeviceCache.List(u.namespace, labels.SelectorFromSet(map[string]string{
		v1.LabelHostname: u.nodeName,
	}))
	if err != nil {
		logrus.Errorf("Failed to add block device via udev event, error: %s, retry in %s", err.Error(), 2*duration)
		u.AddBlockDevice(device, 2*duration, autoProvisioned)
		return
	}
	oldBds := blockdevice.ConvertBlockDevicesToMap(bdList)

	if _, err := u.controller.SaveBlockDevice(device, oldBds, autoProvisioned); err != nil {
		logrus.Errorf("failed to save block device %s, error: %s", device.Name, err.Error())
		//u.AddBlockDevice(device, 2*defaultDuration)
	}
}

// RemoveBlockDevice will set the existing block device to detached state
func (u *Udev) RemoveBlockDevice(device *v1beta1.BlockDevice, udevDevice *Device, disk *block.Disk, duration time.Duration) {
	if duration > defaultDuration {
		time.Sleep(duration)
	}

	logrus.Debugf("uevent remove block deivce %s", device.Spec.DevPath)

	udevDevice.updateDiskFromUdev(disk)
	if guid := block.GenerateDiskGUID(disk, u.nodeName); len(guid) > 0 {
		device.ObjectMeta.Name = guid
	}

	if len(device.Name) == 0 {
		logrus.Infof("Skip removing non-identifiable block device %s", disk.Name)
		return
	}

	err := u.controller.Blockdevices.Delete(u.namespace, device.Name, &metav1.DeleteOptions{})
	if err != nil && errors.IsNotFound(err) {
		logrus.Errorf("failed to delete block device, %s is not found", device.Name)
	} else if err != nil {
		logrus.Errorf("failed to delete block device %s, error: %s", device.Name, err.Error())
		u.RemoveBlockDevice(device, udevDevice, disk, 2*duration)
	}
}

func (u *Udev) deactivateBlockDevice(device *v1beta1.BlockDevice) {
	bd, err := u.controller.BlockdeviceCache.Get(u.namespace, device.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			// Do nothing since the device has already be deleted.
			return
		}

		logrus.Errorf("failed to deactivate block device %s, error: %s", device.Name, err.Error())
		return
	}
	if bd.Status.State == v1beta1.BlockDeviceInactive {
		// Already inactive. Skip...
		return
	}

	logrus.Debugf("deactivate block deivce %s on path %s", bd.Name, bd.Spec.DevPath)

	deviceCpy := bd.DeepCopy()
	deviceCpy.Status.State = v1beta1.BlockDeviceInactive
	if _, err := u.controller.Blockdevices.Update(deviceCpy); err != nil && !errors.IsNotFound(err) {
		logrus.Errorf("failed to deactivate block device %s, error: %s", device.Name, err.Error())
	}
}

// getOptionalMatcher Parse and load config file which contains rules for matching
func getOptionalMatcher(filePath *string) (matcher netlink.Matcher, err error) {
	if filePath == nil || *filePath == "" {
		return nil, nil
	}

	stream, err := ioutil.ReadFile(*filePath)
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
