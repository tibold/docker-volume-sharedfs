// +build linux

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

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

	driver.mutex.Lock()
	defer driver.mutex.Unlock()

	if _, ok := driver.volumes[request.Name]; ok {

		// Path exists and it is already part of the bookkeeping

		message := fmt.Sprintf("Volume %s already exists.", request.Name)
		log.Warning(message)
		return nil

	}

	volumePath := filepath.Join(driver.root, request.Name)

	volume := &sharedVolume{
		Volume: &dockerVolume.Volume{
			Name:       request.Name,
			Mountpoint: volumePath,
			CreatedAt:  time.Now().Format(time.RFC3339),
		},
		Protected: false,
		Exclusive: true,
	}

	if err := volume.loadMetadata(); err != nil {

		if optsProtected, ok := request.Options["protected"]; ok {
			if protected, err := strconv.ParseBool(optsProtected); err == nil {
				volume.Protected = protected
			}
		}

		if optsExclusive, ok := request.Options["exclusive"]; ok {
			if exclusive, err := strconv.ParseBool(optsExclusive); err == nil {
				volume.Exclusive = exclusive
			}
		}

		if err := volume.create(); err != nil {
			return err
		}

		if err = volume.saveMetadata(); err != nil {
			return err
		}
	}

	if err := volume.lock(); err != nil {
		// If the volume cannot be locked, we risk that other nodes may delete it
		return err
	}

	driver.volumes[request.Name] = volume

	if *debug {
		spew.Dump(driver.volumes)
	}

	return nil
}

func (driver sharedVolumeDriver) Discover() {
	if files, err := ioutil.ReadDir(*root); err == nil {
		for _, file := range files {

			filename := file.Name()

			if volume, ok := driver.volumes[filename]; !ok && file.IsDir() {

				volume = &sharedVolume{
					Volume: &dockerVolume.Volume{
						Name:       filename,
						Mountpoint: filepath.Join(*root, filename),
					},
				}

				if volume.hasLockfile() {
					// This volume was locked before.
					if err := volume.loadMetadata(); err == nil {

						// No need to lock the volume. It is already locked.
						driver.volumes[volume.Name] = volume
						log.Infof("Loaded previously attached volume %s", volume.Name)

					} else {
						log.Warningf("Failed to load metadata of previously locked volume %s", filename)
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
