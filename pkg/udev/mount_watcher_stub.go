//go:build !linux

package udev

import "context"

func (u *Udev) watchMounts(_ context.Context) {}
