package block

import (
	"bufio"
	"encoding/hex"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jaypipes/ghw/pkg/block"
	"github.com/jaypipes/ghw/pkg/context"
	"github.com/jaypipes/ghw/pkg/linuxpath"
	"github.com/jaypipes/ghw/pkg/option"
	"github.com/jaypipes/ghw/pkg/util"
	iscsiutil "github.com/longhorn/go-iscsi-helper/util"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/blake2b"

	ndmutil "github.com/harvester/node-disk-manager/pkg/util"
)

// borrowed from https://github.com/jaypipes/ghw/blob/master/pkg/block/block_linux.go
const (
	sectorSize = 512

	FsType = "TYPE"

	PartUUID UUIDType = "PARTUUID"
	PTUUID   UUIDType = "PTUUID"
	UUID     UUIDType = "UUID"
)

type UUIDType string

// Info describes all disk drives and partitions in the host system.
type Info interface {
	GetDisks() []*Disk
	GetPartitions() []*Partition
	GetDiskByDevPath(name string) *Disk
	GetPartitionByDevPath(disk, part string) *Partition
	GetFileSystemInfoByFsUUID(dname string) *FileSystemInfo
}

type infoImpl struct {
	ctx        *context.Context
	Partitions []*Partition `json:"-"`
}

// New returns a pointer to an Info implementation that describes the block
// storage resources of the host system.
func New() (Info, error) {
	isMounted, err := ndmutil.IsHostProcMounted()
	if err != nil {
		return nil, err
	}
	var ctx *context.Context
	if isMounted {
		ctx = context.New(option.WithPathOverrides(option.PathOverrides{
			ndmutil.ProcPath: ndmutil.HostProcPath,
		}))
	} else {
		ctx = context.New()
	}
	info := &infoImpl{ctx: ctx}
	return info, nil
}

func (i *infoImpl) GetDisks() []*Disk {
	paths := linuxpath.New(i.ctx)
	return disks(i.ctx, paths)
}

func (i *infoImpl) GetPartitions() []*Partition {
	return i.Partitions
}

func (i *infoImpl) GetDiskByDevPath(name string) *Disk {
	name = strings.TrimPrefix(name, "/dev/")
	paths := linuxpath.New(i.ctx)
	return getDisk(i.ctx, paths, name)
}

func (i *infoImpl) GetPartitionByDevPath(disk, part string) *Partition {
	disk = strings.TrimPrefix(disk, "/dev/")
	part = strings.TrimPrefix(part, "/dev/")
	paths := linuxpath.New(i.ctx)
	partition := diskPartition(i.ctx, paths, disk, part)
	partition.Disk = getDisk(i.ctx, paths, disk)
	return partition
}

func (i *infoImpl) GetFileSystemInfoByFsUUID(fsUUID string) *FileSystemInfo {
	if fsUUID == "" {
		return nil
	}
	// Resolve the symlink to the special block device file
	dname, err := filepath.EvalSymlinks("/dev/disk/by-uuid/" + fsUUID)
	if err != nil {
		logrus.Errorf("failed to get filesystem info for UUID %s: %s", fsUUID, err.Error())
		return nil
	}
	paths := linuxpath.New(i.ctx)
	mp, pt, ro := partitionInfo(i.ctx, paths, dname)
	return &FileSystemInfo{
		MountPoint: mp,
		Type:       pt,
		IsReadOnly: ro,
	}
}

func diskPhysicalBlockSizeBytes(paths *linuxpath.Paths, disk string) uint64 {
	// We can find the sector size in Linux by looking at the
	// /sys/block/$DEVICE/queue/physical_block_size file in sysfs
	path := filepath.Join(paths.SysBlock, disk, "queue", "physical_block_size")
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return 0
	}
	size, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		return 0
	}
	return size
}

func diskSizeBytes(paths *linuxpath.Paths, disk string) uint64 {
	// We can find the number of 512-byte sectors by examining the contents of
	// /sys/block/$DEVICE/size and calculate the physical bytes accordingly.
	path := filepath.Join(paths.SysBlock, disk, "size")
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return 0
	}
	size, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		return 0
	}
	return size * sectorSize
}

func diskNUMANodeID(paths *linuxpath.Paths, disk string) int {
	link, err := os.Readlink(filepath.Join(paths.SysBlock, disk))
	if err != nil {
		return -1
	}
	for partial := link; strings.HasPrefix(partial, "../devices/"); partial = filepath.Base(partial) {
		if nodeContents, err := ioutil.ReadFile(filepath.Join(paths.SysBlock, partial, "numa_node")); err != nil {
			if nodeInt, err := strconv.Atoi(string(nodeContents)); err != nil {
				return nodeInt
			}
		}
	}
	return -1
}

func diskVendor(paths *linuxpath.Paths, disk string) string {
	// In Linux, the vendor for a disk device is found in the
	// /sys/block/$DEVICE/device/vendor file in sysfs
	path := filepath.Join(paths.SysBlock, disk, "device", "vendor")
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return util.UNKNOWN
	}
	return strings.TrimSpace(string(contents))
}

func udevInfo(paths *linuxpath.Paths, disk string) (map[string]string, error) {
	// Get device major:minor numbers
	devNo, err := ioutil.ReadFile(filepath.Join(paths.SysBlock, disk, "dev"))
	if err != nil {
		return nil, err
	}

	// Look up block device in udev runtime database
	udevID := "b" + strings.TrimSpace(string(devNo))
	udevBytes, err := ioutil.ReadFile(filepath.Join(paths.RunUdevData, udevID))
	if err != nil {
		return nil, err
	}

	udevInfo := make(map[string]string)
	for _, udevLine := range strings.Split(string(udevBytes), "\n") {
		if strings.HasPrefix(udevLine, "E:") {
			if s := strings.SplitN(udevLine[2:], "=", 2); len(s) == 2 {
				udevInfo[s[0]] = s[1]
			}
		}
	}
	return udevInfo, nil
}

func diskModel(paths *linuxpath.Paths, disk string) string {
	info, err := udevInfo(paths, disk)
	if err != nil {
		return util.UNKNOWN
	}

	if model, ok := info["ID_MODEL"]; ok {
		return model
	}
	return util.UNKNOWN
}

func diskSerialNumber(paths *linuxpath.Paths, disk string) string {
	info, err := udevInfo(paths, disk)
	if err != nil {
		return util.UNKNOWN
	}

	// There are two serial number keys, ID_SERIAL and ID_SERIAL_SHORT The
	// non-_SHORT version often duplicates vendor information collected
	// elsewhere, so use _SHORT and fall back to ID_SERIAL if missing...
	if serial, ok := info["ID_SERIAL_SHORT"]; ok {
		return serial
	}
	if serial, ok := info["ID_SERIAL"]; ok {
		return serial
	}
	return util.UNKNOWN
}

func diskBusPath(paths *linuxpath.Paths, disk string) string {
	info, err := udevInfo(paths, disk)
	if err != nil {
		return util.UNKNOWN
	}

	// There are two path keys, ID_PATH and ID_PATH_TAG.
	// The difference seems to be _TAG has funky characters converted to underscores.
	if path, ok := info["ID_PATH"]; ok {
		return path
	}
	return util.UNKNOWN
}

func diskWWN(paths *linuxpath.Paths, disk string) string {
	info, err := udevInfo(paths, disk)
	if err != nil {
		return util.UNKNOWN
	}

	// Trying ID_WWN_WITH_EXTENSION and falling back to ID_WWN is the same logic lsblk uses
	if wwn, ok := info["ID_WWN_WITH_EXTENSION"]; ok {
		return wwn
	}
	if wwn, ok := info["ID_WWN"]; ok {
		return wwn
	}
	return util.UNKNOWN
}

// diskPartitions takes the name of a disk (note: *not* the path of the disk,
// but just the name. In other words, "sda", not "/dev/sda" and "nvme0n1" not
// "/dev/nvme0n1") and returns a slice of pointers to Partition structs
// representing the partitions in that disk
func diskPartitions(ctx *context.Context, paths *linuxpath.Paths, disk string) []*Partition {
	out := make([]*Partition, 0)
	path := filepath.Join(paths.SysBlock, disk)
	files, err := ioutil.ReadDir(path)
	if err != nil {
		ctx.Warn("failed to read disk partitions: %s\n", err)
		return out
	}
	for _, file := range files {
		fname := file.Name()
		if !strings.HasPrefix(fname, disk) {
			continue
		}
		p := diskPartition(ctx, paths, disk, fname)
		out = append(out, p)
	}
	return out
}

func diskPartition(ctx *context.Context, paths *linuxpath.Paths, disk, fname string) *Partition {
	size := partitionSizeBytes(paths, disk, fname)
	mp, pt, ro := partitionInfo(ctx, paths, fname)
	du := GetDiskUUID(fname, string(PartUUID))
	fsUUID := GetDiskUUID(fname, string(UUID))
	driveType, storageController := diskTypes(fname)
	label := GetFileSystemLabel(fname)
	partType := GetPartType(fname)
	return &Partition{
		Name:      fname,
		Label:     label,
		SizeBytes: size,
		FileSystemInfo: FileSystemInfo{
			MountPoint: mp,
			Type:       pt,
			IsReadOnly: ro,
		},
		UUID:              du,
		FsUUID:            fsUUID,
		PartType:          partType,
		DriveType:         driveType,
		StorageController: storageController,
	}
}

func diskIsRemovable(paths *linuxpath.Paths, disk string) bool {
	path := filepath.Join(paths.SysBlock, disk, "removable")
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return false
	}
	removable := strings.TrimSpace(string(contents))
	if removable == "1" {
		return true
	}
	return false
}

func getDisk(ctx *context.Context, paths *linuxpath.Paths, dname string) *Disk {
	driveType, storageController := diskTypes(dname)
	// TODO(jaypipes): Move this into diskTypes() once abstracting
	// diskIsRotational for ease of unit testing
	if !diskIsRotational(ctx, paths, dname) {
		driveType = block.DRIVE_TYPE_SSD
	}
	size := diskSizeBytes(paths, dname)
	pbs := diskPhysicalBlockSizeBytes(paths, dname)
	busPath := diskBusPath(paths, dname)
	node := diskNUMANodeID(paths, dname)
	vendor := diskVendor(paths, dname)
	model := diskModel(paths, dname)
	serialNo := diskSerialNumber(paths, dname)
	wwn := diskWWN(paths, dname)
	removable := diskIsRemovable(paths, dname)
	uuid := GetDiskUUID(dname, string(UUID))
	ptuuid := GetDiskUUID(dname, string(PTUUID))
	mp, pt, ro := partitionInfo(ctx, paths, dname)
	fs := FileSystemInfo{
		MountPoint: mp,
		Type:       pt,
		IsReadOnly: ro,
	}

	if fs.Type == "" {
		fs.Type = GetFileSystemType(dname)
	}

	d := &Disk{
		Name:                   dname,
		SizeBytes:              size,
		PhysicalBlockSizeBytes: pbs,
		DriveType:              driveType,
		IsRemovable:            removable,
		StorageController:      storageController,
		UUID:                   uuid,
		PtUUID:                 ptuuid,
		BusPath:                busPath,
		NUMANodeID:             node,
		Vendor:                 vendor,
		Model:                  model,
		SerialNumber:           serialNo,
		WWN:                    wwn,
		FileSystemInfo:         fs,
	}

	parts := diskPartitions(ctx, paths, dname)
	// Map this Disk object into the Partition...
	for _, part := range parts {
		part.Disk = d
	}
	d.Partitions = parts

	return d
}

func disks(ctx *context.Context, paths *linuxpath.Paths) []*Disk {
	// In Linux, we could use the fdisk, lshw or blockdev commands to list disk
	// information, however all of these utilities require root privileges to
	// run. We can get all of this information by examining the /sys/block
	// and /sys/class/block files
	disks := make([]*Disk, 0)
	files, err := ioutil.ReadDir(paths.SysBlock)
	if err != nil {
		return nil
	}
	for _, file := range files {
		dname := file.Name()
		if strings.HasPrefix(dname, "loop") {
			continue
		}

		d := getDisk(ctx, paths, dname)
		disks = append(disks, d)
	}

	return disks
}

// diskTypes returns the drive type, storage controller and bus type of a disk
func diskTypes(dname string) (block.DriveType, block.StorageController) {
	// The conditionals below which set the controller and drive type are
	// based on information listed here:
	// https://en.wikipedia.org/wiki/Device_file
	driveType := block.DRIVE_TYPE_UNKNOWN
	storageController := block.STORAGE_CONTROLLER_UNKNOWN
	if strings.HasPrefix(dname, "fd") {
		driveType = block.DRIVE_TYPE_FDD
	} else if strings.HasPrefix(dname, "sd") {
		driveType = block.DRIVE_TYPE_HDD
		storageController = block.STORAGE_CONTROLLER_SCSI
	} else if strings.HasPrefix(dname, "hd") {
		driveType = block.DRIVE_TYPE_HDD
		storageController = block.STORAGE_CONTROLLER_IDE
	} else if strings.HasPrefix(dname, "vd") {
		driveType = block.DRIVE_TYPE_HDD
		storageController = block.STORAGE_CONTROLLER_VIRTIO
	} else if strings.HasPrefix(dname, "nvme") {
		driveType = block.DRIVE_TYPE_SSD
		storageController = block.STORAGE_CONTROLLER_NVME
	} else if strings.HasPrefix(dname, "sr") {
		driveType = block.DRIVE_TYPE_ODD
		storageController = block.STORAGE_CONTROLLER_SCSI
	} else if strings.HasPrefix(dname, "xvd") {
		driveType = block.DRIVE_TYPE_HDD
		storageController = block.STORAGE_CONTROLLER_SCSI
	} else if strings.HasPrefix(dname, "mmc") {
		driveType = block.DRIVE_TYPE_SSD
		storageController = block.STORAGE_CONTROLLER_MMC
	}

	return driveType, storageController
}

func diskIsRotational(ctx *context.Context, paths *linuxpath.Paths, devName string) bool {
	path := filepath.Join(paths.SysBlock, devName, "queue", "rotational")
	contents := util.SafeIntFromFile(ctx, path)
	return contents == 1
}

// partitionSizeBytes returns the size in bytes of the partition given a disk
// name and a partition name. Note: disk name and partition name do *not*
// contain any leading "/dev" parts. In other words, they are *names*, not
// paths.
func partitionSizeBytes(paths *linuxpath.Paths, disk string, part string) uint64 {
	path := filepath.Join(paths.SysBlock, disk, part, "size")
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return 0
	}
	size, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		return 0
	}
	return size * sectorSize
}

// Given a full or short partition name, returns the mount point, the type of
// the partition and whether it's readonly
func partitionInfo(ctx *context.Context, paths *linuxpath.Paths, part string) (string, string, bool) {
	// Allow calling PartitionInfo with either the full partition name
	// "/dev/sda1" or just "sda1"
	if !strings.HasPrefix(part, "/dev") {
		part = "/dev/" + part
	}

	// mount entries for mounted partitions look like this:
	// /dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0
	var r io.ReadCloser
	r, err := openProcMounts(ctx, paths)
	if err != nil {
		return "", "", true
	}
	defer util.SafeClose(r)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		entry := parseMountEntry(line)
		if entry == nil || entry.Partition != part {
			continue
		}
		ro := true
		for _, opt := range entry.Options {
			if opt == "rw" {
				ro = false
				break
			}
		}

		return entry.Mountpoint, entry.FilesystemType, ro
	}
	return "", "", true
}

func openProcMounts(ctx *context.Context, paths *linuxpath.Paths) (*os.File, error) {
	file := paths.ProcMounts
	if path, ok := ctx.PathOverrides[ndmutil.ProcPath]; ok {
		ns := iscsiutil.GetHostNamespacePath(path)
		file = strings.TrimSuffix(ns, "ns/") + "mounts"
	}
	return os.Open(file)
}

type mountEntry struct {
	Partition      string
	Mountpoint     string
	FilesystemType string
	Options        []string
}

func parseMountEntry(line string) *mountEntry {
	// mount entries for mounted partitions look like this:
	// /dev/sda6 / ext4 rw,relatime,errors=remount-ro,data=ordered 0 0
	if line[0] != '/' {
		return nil
	}
	fields := strings.Fields(line)

	if len(fields) < 4 {
		return nil
	}

	// We do some special parsing of the mountpoint, which may contain space,
	// tab and newline characters, encoded into the mount entry line using their
	// octal-to-string representations. From the GNU mtab man pages:
	//
	//   "Therefore these characters are encoded in the files and the getmntent
	//   function takes care of the decoding while reading the entries back in.
	//   '\040' is used to encode a space character, '\011' to encode a tab
	//   character, '\012' to encode a newline character, and '\\' to encode a
	//   backslash."
	mp := fields[1]
	r := strings.NewReplacer(
		"\\011", "\t", "\\012", "\n", "\\040", " ", "\\\\", "\\",
	)
	mp = r.Replace(mp)

	res := &mountEntry{
		Partition:      fields[0],
		Mountpoint:     mp,
		FilesystemType: fields[2],
	}
	opts := strings.Split(fields[3], ",")
	res.Options = opts
	return res
}

// GeneratePartitionGUID generates a GUID for partitions.
func GeneratePartitionGUID(part *Partition, nodeName string) string {
	if valueExists(part.UUID) {
		return makeHashGUID(nodeName + part.UUID)
	}
	logrus.Warnf("failed to generate GUID for device %s", part.Name)
	return ""
}

// GenerateDiskGUID generates a GUID for disks.
func GenerateDiskGUID(disk *Disk, nodeName string) string {
	var id string
	if valueExists(disk.WWN) {
		id = disk.WWN + disk.Vendor + disk.Model + disk.SerialNumber
	} else if valueExists(disk.UUID) {
		id = disk.UUID
	} else if valueExists(disk.PtUUID) {
		id = disk.PtUUID
	}
	if valueExists(id) {
		return makeHashGUID(nodeName + id)
	}
	logrus.Warnf("failed to generate GUID for device %s", disk.Name)
	return ""
}

func makeHashGUID(payload string) string {
	hasher, _ := blake2b.New(16, nil)
	hasher.Write([]byte(payload))
	return hex.EncodeToString(hasher.Sum(nil))
}

func valueExists(value string) bool {
	return len(value) > 0 && value != util.UNKNOWN
}
