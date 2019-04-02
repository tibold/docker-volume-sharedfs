// +build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	dockerVolume "github.com/docker/go-plugins-helpers/volume"
)

// A single volume instance
type sharedVolume struct {
	*dockerVolume.Volume
	Protected bool
	Exclusive bool
}

func (volume *sharedVolume) GetDataDir() string {
	return filepath.Join(volume.Mountpoint, "_data")
}

func (volume *sharedVolume) GetLocksDir() string {
	return filepath.Join(volume.Mountpoint, "_locks")
}

func (volume *sharedVolume) GetLockFile() string {
	return filepath.Join(volume.Mountpoint, "_locks", fmt.Sprintf("%s.lock", *hostname))
}

func (volume *sharedVolume) GetMountFile(id string) string {
	var mountFile string
	if volume.Exclusive {
		mountFile = filepath.Join(volume.Mountpoint, "_locks", "exclusive.mount")
	} else {
		mountFile = filepath.Join(volume.Mountpoint, "_locks", fmt.Sprintf("%s.mount", id))
	}

	return mountFile
}

// Creates the directory structure needed for the volume
func (volume *sharedVolume) create() error {

	fstat, err := os.Lstat(volume.Mountpoint)

	if os.IsNotExist(err) {
		err = os.Mkdir(volume.Mountpoint, 0750)
	}

	if fstat != nil && !fstat.IsDir() {
		return fmt.Errorf("%v already exist and it's not a directory", volume.Mountpoint)
	}

	if err == nil {
		dataDir := volume.GetDataDir()
		if _, err = os.Lstat(dataDir); err == nil {
			err = os.Mkdir(dataDir, 750)
		}
	}

	if err == nil {
		locksDir := volume.GetLocksDir()
		if _, err = os.Lstat(locksDir); err == nil {
			err = os.Mkdir(locksDir, 750)
		}
	}

	if err != nil {
		return err
	}

	return nil
}

func (volume *sharedVolume) delete() error {
	var err error

	// Reload the metadata to make sure no-one changed it.
	volume.loadMetadata()

	if volume.Protected {
		return nil
	}

	if _, err = os.Stat(volume.Mountpoint); os.IsNotExist(err) {
		return nil
	} else if locked, err := volume.isLocked(); !locked && err == nil {
		err = os.RemoveAll(volume.Mountpoint)
	}

	return err
}

// Returns true if any node has locked the volume
func (volume *sharedVolume) isLocked() (bool, error) {
	locksDir := volume.GetLocksDir()

	files, err := ioutil.ReadDir(locksDir)
	if err != nil {
		return false, err
	}

	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".lock" {
			// We are only interested if such a file exists
			return true, nil
		}
	}

	return false, nil
}

func (volume *sharedVolume) hasLockfile() bool {
	lockFile := volume.GetLockFile()

	file, err := os.Stat(lockFile)
	return err == nil && !file.IsDir()
}

func (volume *sharedVolume) getLocks() []string {
	locksDir := volume.GetLocksDir()

	files, err := ioutil.ReadDir(locksDir)
	if err != nil {
		return nil
	}

	locks := make([]string, 0, len(files))

	for _, file := range files {
		name := file.Name()
		if !file.IsDir() && filepath.Ext(name) == ".lock" {
			lock := name[0 : len(name)-len(".lock")]
			locks = append(locks, lock)
		}
	}

	return locks
}

// Locks the volume
func (volume *sharedVolume) lock() error {
	var err error

	lockFilename := volume.GetLockFile()

	if file, err := os.OpenFile(lockFilename, os.O_RDONLY|os.O_CREATE, 0600); err == nil {
		file.Close()
	}

	return err
}

// Unlocks the volume
func (volume *sharedVolume) unlock() error {
	var err error

	lockFilename := volume.GetLockFile()

	if _, err = os.Stat(lockFilename); err == nil {
		err = os.Remove(lockFilename)
	}

	return err
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

func (volume *sharedVolume) getMounts() map[string]string {
	locksDir := volume.GetLocksDir()

	files, err := ioutil.ReadDir(locksDir)
	if err != nil {
		return nil
	}

	mounts := make(map[string]string)

	for _, file := range files {
		fileName := file.Name()
		if !file.IsDir() && filepath.Ext(fileName) == ".mount" {
			mountName := fileName[0 : len(fileName)-len(".mount")]

			fileFullPath := filepath.Join(locksDir, fileName)

			if contents, err := ioutil.ReadFile(fileFullPath); err == nil {
				mountedHostname := string(contents)

				mounts[mountName] = mountedHostname
			}
		}
	}

	return mounts
}

func (volume *sharedVolume) mount(id string) error {
	mountFile := volume.GetMountFile(id)

	if file, err := os.OpenFile(mountFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); err == nil {
		written, err := file.WriteString(*hostname)

		if err == nil {
			if written != len(*hostname) {
				err = io.ErrShortWrite
			} else {
				err = file.Close()
			}
		}

		if err != nil {
			return err
		}
	} else {
		return fmt.Errorf("Volume %s is already mounted", volume.Name)
	}

	return nil
}

func (volume *sharedVolume) unmount(id string) error {
	var err error
	mountFile := volume.GetMountFile(id)

	if contents, err := ioutil.ReadFile(mountFile); err == nil {

		mountedHostname := string(contents)
		if mountedHostname == *hostname {
			err = os.Remove(mountFile)
		} else {
			err = fmt.Errorf("Volume %s is mounted by %s host", volume.Name, mountedHostname)
		}

	} else if os.IsNotExist(err) {
		err = nil
	}

	return err
}

// Saves the volume metadata into a file
func (volume *sharedVolume) saveMetadata() error {
	var file *os.File
	metaFile := filepath.Join(volume.Mountpoint, "meta.json")

	content, err := json.MarshalIndent(volume, "", "  ")
	if err == nil {
		// Creating a meta file only if it does not yet exist.
		// This should stop concurrency issues when creating 2 volume with the same name and different options
		if file, err = os.OpenFile(metaFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); err == nil {
			var count int
			count, err = file.Write(content)
			if count < len(content) {
				err = io.ErrShortWrite
			}
		}
	}

	return err
}

// Loads the volume metadata from a file
func (volume *sharedVolume) loadMetadata() error {

	metaFile := filepath.Join(volume.Mountpoint, "meta.json")

	content, err := ioutil.ReadFile(metaFile)
	if err == nil {
		err = json.Unmarshal([]byte(content), &volume)
	}
	return err
}
