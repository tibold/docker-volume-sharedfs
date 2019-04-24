// +build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"time"

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
	return volume.GetLockFileFor(*hostname)
}

func (volume *sharedVolume) GetLockFileFor(name string) string {
	return filepath.Join(volume.Mountpoint, "_locks", fmt.Sprintf("%s.lock", name))
}

func (driver *sharedVolumeDriver) newVolume(name string) *sharedVolume {

	// Get the absolute volume path
	volumePath := filepath.Join(driver.root, name)

	// Register a new volume
	volume := &sharedVolume{
		Volume: &dockerVolume.Volume{
			Name:       name,
			Mountpoint: volumePath,
			CreatedAt:  time.Now().Format(time.RFC3339),
		},
		Protected: false,
		Exclusive: false,
	}

	return volume
}

func (volume *sharedVolume) parseOptions(options map[string]string) {

	// Parse 'protected' option
	if optsProtected, ok := options["protected"]; ok {
		if protected, err := strconv.ParseBool(optsProtected); err == nil {
			volume.Protected = protected
		}
	}

	// Parse 'exclusive' option
	if optsExclusive, ok := options["exclusive"]; ok {
		if exclusive, err := strconv.ParseBool(optsExclusive); err == nil {
			volume.Exclusive = exclusive
		}
	}
}

// Creates the directory structure needed for the volume
func (volume *sharedVolume) createDirectoryStructure() error {

	fstat, err := os.Lstat(volume.Mountpoint)

	if os.IsNotExist(err) {
		err = os.Mkdir(volume.Mountpoint, 0750)
	}

	if fstat != nil && !fstat.IsDir() {
		return fmt.Errorf("%v already exist and it's not a directory", volume.Mountpoint)
	}

	if err == nil {
		dataDir := volume.GetDataDir()
		if _, err = os.Lstat(dataDir); os.IsNotExist(err) {
			err = os.Mkdir(dataDir, 750)
		}
	}

	if err == nil {
		locksDir := volume.GetLocksDir()
		if _, err = os.Lstat(locksDir); os.IsNotExist(err) {
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
