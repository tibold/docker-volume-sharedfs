package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	log "github.com/Sirupsen/logrus"
)

// Keep track of who has mounted the volume
type volumeMount struct {
	LockFilePath string `json:"-"`
	MountID      string
	Hostname     string
}

// Load the mount info from file
func (mount *volumeMount) load() error {
	content, err := ioutil.ReadFile(mount.LockFilePath)

	if err == nil {
		err = json.Unmarshal([]byte(content), mount)
	}

	return err
}

// Save the mount info to file
func (mount *volumeMount) save() error {

	content, err := json.MarshalIndent(mount, "", "  ")
	if err != nil {
		return err
	}

	file, err := os.OpenFile(mount.LockFilePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}

	written, err := file.Write(content)
	file.Close()

	if err == nil && written != len(content) {
		os.Remove(mount.LockFilePath)

		err = io.ErrShortWrite
	}
	return err
}

// Remove the mount lock file
func (mount *volumeMount) remove() error {

	if err := os.Remove(mount.LockFilePath); !os.IsNotExist(err) {
		return err
	}

	return nil
}

func (volume *sharedVolume) getMountFile(id string) string {
	var mountFile string
	if volume.Exclusive {
		mountFile = filepath.Join(volume.Mountpoint, "_locks", "exclusive.mount")
	} else {
		mountFile = filepath.Join(volume.Mountpoint, "_locks", fmt.Sprintf("%s.mount", id))
	}

	return mountFile
}

// Returns true if any node has mounted the volume
func (volume *sharedVolume) isMounted() (bool, error) {
	locksDir := volume.GetLocksDir()

	files, err := ioutil.ReadDir(locksDir)
	if err != nil {
		return false, err
	}

	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".mount" {
			// We are only interested if such a file exists
			return true, nil
		}
	}

	return false, nil
}

func (volume *sharedVolume) getMounts() map[string]*volumeMount {
	locksDir := volume.GetLocksDir()

	files, err := ioutil.ReadDir(locksDir)
	if err != nil {
		return nil
	}

	mounts := make(map[string]*volumeMount)

	for _, file := range files {
		fileName := file.Name()
		if !file.IsDir() && filepath.Ext(fileName) == ".mount" {

			mountID := fileName[0 : len(fileName)-len(".mount")]

			if mount, err := volume.loadMount(mountID); err == nil {

				mounts[mountID] = mount
			} else {
				log.Errorf("Failed to read mount file for %s", mountID)
			}
		}
	}

	return mounts
}

// LoadFrom a file
func (volume *sharedVolume) newMount(id string) *volumeMount {

	mount := &volumeMount{
		LockFilePath: volume.getMountFile(id),
		MountID:      id,
		Hostname:     *hostname,
	}

	return mount
}

// LoadFrom a file
func (volume *sharedVolume) loadMount(id string) (*volumeMount, error) {

	mount := &volumeMount{
		LockFilePath: volume.getMountFile(id),
	}

	if err := mount.load(); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	return mount, nil
}

func (volume *sharedVolume) mount(id string) error {
	var err error
	newMount := volume.newMount(id)

	if err = newMount.save(); err == nil {
		// FastPath, mount acquired
		return nil
	}

	// Failed to acquire mount, try slow path
	// Let's investigate
	var mount *volumeMount
	var lock *volumeLock

	// The lock keepalive seems to be either late or the other node is dead.
	// Worth to wait a little and see...

	tryUntil := time.Now().Add(lockTimeout)

	for time.Now().Before(tryUntil) {

		time.Sleep(time.Second * 5)

		// Who has the mount:
		mount, err = volume.loadMount(id)
		if err != nil {
			// We already own the mount by us...
			return fmt.Errorf("Failed to load mount info for %s", volume.Name)
		}

		// The mount file might be gone already
		if mount != nil {

			if mount.MountID == id {
				// The same ID already owns the mount.
				// It shouldn't happen.
				return nil

			} else if mount.Hostname == *hostname {
				// We already own the mount by us...
				// And because it is us, there is little point in trying to wait for a timeout
				return fmt.Errorf("Volume %s is already mounted on the same host", volume.Name)
			}

			// Check the host's lockfile and look for a timeout
			lock, err = volume.getLock(mount.Hostname)
			if err != nil {
				log.Errorf("Failed to load lock file for volume %s", volume.Name)
				continue
			}

			// If the file exist, did it time out already?
			if lock != nil {
				if ok, err := lock.tryUnlock(); err != nil {
					// Something went wrong during remove
					// This is not normal operation
					return err
				} else if ok {
					// The lock file was timed out, so we have removed it
					lock = nil
				}
			}

			// If the lock file does not exist, or was removed
			if lock == nil {
				// Try to remove the mount file
				err = mount.remove()
				if err != nil {
					// The mount is not actually locked, something else is wrong here...
					return err
				}
			}
		}

		if err = newMount.save(); !os.IsExist(err) {
			// If the error is not about the file existing, than return
			// This could also mean success BTW
			return err
		}
	}

	return err
}

func (volume *sharedVolume) unmount(id string) error {
	var err error
	mount, err := volume.loadMount(id)
	if os.IsNotExist(err) {
		log.Warnf("Trying to unmount a volume that is not mounted")
	} else if err != nil {
		return err
	}

	if mount.MountID != id {
		log.Errorf("Trying to unmount a volume that is mounted for a different id")
	} else if mount.Hostname != *hostname {
		log.Errorf("Trying to unmount a volume that is mounted for a different host")
	} else {
		err = mount.remove()
	}

	return err
}
