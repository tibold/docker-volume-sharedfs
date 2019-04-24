package main

import (
	"time"
)

func (driver sharedVolumeDriver) MaintenanceRoutine() {

	lockTicker := time.NewTicker(lockInterval)
	cleanupTicker := time.NewTicker(cleanupInterval)

	for {

		select {
		case <-lockTicker.C:
			driver.RefreshLocks()
		case <-cleanupTicker.C:
			driver.Cleanup()
		}
	}
}

func (driver sharedVolumeDriver) RefreshLocks() {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()

	for _, volume := range driver.volumes {
		volume.lock()
	}
}

// For each volume remove mounts that
func (driver sharedVolumeDriver) Cleanup() {
	driver.mutex.Lock()
	defer driver.mutex.Unlock()

	for _, volume := range driver.volumes {

		locks := volume.getLocks()

		for id, lock := range locks {
			if ok, _ := lock.tryUnlock(); ok {
				delete(locks, id)
			}
		}

		mounts := volume.getMounts()

		for _, mount := range mounts {
			if _, ok := locks[mount.Hostname]; !ok {
				mount.remove()
			}
		}
	}
}
