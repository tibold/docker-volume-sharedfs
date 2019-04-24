// +build linux

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	log "github.com/Sirupsen/logrus"

	"github.com/davecgh/go-spew/spew"
	dockerVolume "github.com/docker/go-plugins-helpers/volume"
)

type sharedVolumeDriver struct {
	volumes  map[string]*sharedVolume
	mutex    *sync.Mutex
	root     string
	hostname string
}

func newSharedFSDriver(root string) sharedVolumeDriver {
	hostname, _ := os.Hostname()

	driver := sharedVolumeDriver{
		volumes:  make(map[string]*sharedVolume),
		mutex:    &sync.Mutex{},
		root:     root,
		hostname: hostname,
	}

	// Discover volumes that are already in use by the current node
	driver.Discover()

	go driver.MaintenanceRoutine()

	return driver
}

func (driver sharedVolumeDriver) Capabilities() *dockerVolume.CapabilitiesResponse {
	return &dockerVolume.CapabilitiesResponse{
		Capabilities: dockerVolume.Capability{
			Scope: "global",
		},
	}
}

func (driver sharedVolumeDriver) Create(request *dockerVolume.CreateRequest) error {
	// var volume *sharedVolume

	log.Infof("Create: %s, %v", request.Name, request.Options)

	// Concurrency lock
	driver.mutex.Lock()
	defer driver.mutex.Unlock()

	// Is this volume already registered?
	if _, ok := driver.volumes[request.Name]; ok {

		// Path exists and it is already part of the bookkeeping
		message := fmt.Sprintf("Volume %s already exists.", request.Name)
		log.Warning(message)
		return nil

	}

	// Register a new volume
	volume := driver.newVolume(request.Name)
	var err error

	// Does the volume exist already?
	if err = volume.loadMetadata(); os.IsNotExist(err) {
		// The volume does not yet exist

		// Create volume (physical folder structure)
		if err = volume.createDirectoryStructure(); err != nil {
			return err
		}

		// Create a new one if not
		volume.parseOptions(request.Options)

		// Save the volume metadata
		if err = volume.saveMetadata(); os.IsExist(err) {
			// Failed to save metadata, because the file already exists

			// Try to load the metadata again
			err = volume.loadMetadata()
		}
	}

	if err != nil {
		return err
	}

	// Try to lock the volume
	if err := volume.lock(); err != nil {
		// If the volume cannot be locked, we risk that other nodes may delete it
		return err
	}

	// Register in internal bookkeeping
	driver.volumes[request.Name] = volume

	if *debug {
		spew.Dump(driver.volumes)
	}

	return nil
}

func (driver sharedVolumeDriver) Discover() {
	// Look for existing volumes
	if files, err := ioutil.ReadDir(*root); err == nil {
		for _, file := range files {

			filename := file.Name()

			if !file.IsDir() {
				// Only intersted in directories.
				continue
			}

			// Is this volume registered in bookkeeping already?
			if volume, ok := driver.volumes[filename]; !ok {

				// Try to load the volume information
				volume = &sharedVolume{
					Volume: &dockerVolume.Volume{
						Name:       filename,
						Mountpoint: filepath.Join(*root, filename),
					},
				}

				if err := volume.loadMetadata(); err != nil {

					// Metadata failed to load, it's likely invalid
					log.Errorf("Failed to load metadata for volume %s", volume.Name)

				} else {

					// If there is a lockfile add it to bookkeeping
					if volume.hasLockfile() {

						driver.volumes[volume.Name] = volume
						log.Infof("Loaded previously attached volume %s", volume.Name)
					}

					// Remove any mounts that belonged to us
					mounts := volume.getMounts()
					for _, mount := range mounts {
						if mount.Hostname == *hostname {
							mount.remove()
						}
					}
				}
			}
		}
	}
}

func (driver sharedVolumeDriver) Remove(request *dockerVolume.RemoveRequest) error {
	log.Infof("Remove: %s", request.Name)

	driver.mutex.Lock()
	defer driver.mutex.Unlock()

	if volume, ok := driver.volumes[request.Name]; ok {

		err := volume.unlock()

		if err == nil {
			err = volume.delete()
		}

		if err == nil {
			delete(driver.volumes, request.Name)
		} else {
			return err
		}
	}

	return nil
}

func (driver sharedVolumeDriver) Path(request *dockerVolume.PathRequest) (*dockerVolume.PathResponse, error) {
	log.Debugf("Path: %s", request.Name)

	if volume, ok := driver.volumes[request.Name]; ok {

		return &dockerVolume.PathResponse{
			Mountpoint: volume.GetDataDir(),
		}, nil
	}

	return nil, nil
}

func (driver sharedVolumeDriver) Mount(request *dockerVolume.MountRequest) (*dockerVolume.MountResponse, error) {
	log.Infof("Mount: %s", request.Name)

	if volume, ok := driver.volumes[request.Name]; ok {

		if err := volume.mount(request.ID); err != nil {
			return nil, fmt.Errorf("Failed to mount volume: %s", err.Error())
		}

		return &dockerVolume.MountResponse{
			Mountpoint: volume.GetDataDir(),
		}, nil
	}

	message := fmt.Sprintf("Cannot mount volume %s as it does not exist in the bookkeeping", request.Name)

	log.Error(message)

	return nil, errors.New(message)
}

func (driver sharedVolumeDriver) Unmount(request *dockerVolume.UnmountRequest) error {
	log.Infof("Unmount: %s", request.Name)

	if volume, ok := driver.volumes[request.Name]; ok {
		err := volume.unmount(request.ID)
		return err
	}

	return nil
}

func (driver sharedVolumeDriver) Get(request *dockerVolume.GetRequest) (*dockerVolume.GetResponse, error) {
	log.Infof("Get: %s", request.Name)

	if volume, ok := driver.volumes[request.Name]; ok {
		responseVolume := &dockerVolume.Volume{
			Name:       volume.Name,
			Mountpoint: volume.GetDataDir(),
			CreatedAt:  volume.CreatedAt,
			Status:     make(map[string]interface{}),
		}

		responseVolume.Status["protected"] = volume.Protected
		responseVolume.Status["exclusive"] = volume.Exclusive
		responseVolume.Status["locks"] = volume.getLocks()
		responseVolume.Status["mounts"] = volume.getMounts()

		return &dockerVolume.GetResponse{
			Volume: responseVolume,
		}, nil
	}

	return nil, fmt.Errorf("volume %s unknown", request.Name)
}

func (driver sharedVolumeDriver) List() (*dockerVolume.ListResponse, error) {
	log.Infof("List")

	volumes := []*dockerVolume.Volume{}

	for _, volume := range driver.volumes {
		volumes = append(volumes, &dockerVolume.Volume{
			Name:       volume.Name,
			Mountpoint: volume.GetDataDir(),
		})
	}

	return &dockerVolume.ListResponse{Volumes: volumes}, nil
}
