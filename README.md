node-disk-manager
========

disk manager help to manage host disks, implementing disk partition and file system formatting.

## Building

`make`

## Running

`./bin/node-disk-manager`

## Features

- [x] Disk provisioning as Longhorn disks with a simple boolean.
- [x] Disk formatting if needed with a simple boolean.
- [x] Disk discovery, including existing block devices, and hot plugged disks.
- [x] Support multiple storage controller (IDE/SATA/SCSI/Virtio).
- [x] Support vritual disks (WWN on the disk is required for unique identification).
- [ ] Device mapper and LVM are not yet supported.
- [ ] The behaviour of multipath devices is undefined.

## License
Copyright (c) 2022 [Rancher Labs, Inc.](http://rancher.com)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
