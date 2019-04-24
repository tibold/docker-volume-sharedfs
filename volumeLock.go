package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	log "github.com/Sirupsen/logrus"
)

// Locking the volume into existence.
// As long as any host has the volume locked
// the rest of the hosts will not delete the data files.
type volumeLock struct {
	volume       *sharedVolume
	lockFilename string
	hostname     string
	lockedTime   time.Time
}

func (lock *volumeLock) age() time.Duration {
	return time.Now().UTC().Sub(lock.lockedTime)
}

func (lock *volumeLock) tryUnlock() (bool, error) {

	lockAge := lock.age()
	if lockAge < lockTimeout {
		return false, nil
	}

	if err := lock.remove(); err != nil {
		return false, err
	}

	return true, nil
}

func (lock *volumeLock) remove() error {
	return os.Remove(lock.lockFilename)
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

func (volume *sharedVolume) getLocks() map[string]*volumeLock {
	locksDir := volume.GetLocksDir()

	files, err := ioutil.ReadDir(locksDir)
	if err != nil {
		return nil
	}

	locks := make(map[string]*volumeLock)

	for _, file := range files {
		name := file.Name()
		if !file.IsDir() && filepath.Ext(name) == ".lock" {
			host := name[0 : len(name)-len(".lock")]

			if lock, err := volume.getLock(host); err == nil {
				locks[host] = lock
			} else {
				log.Errorf("Failed to read lock file %s", host)
			}
		}
	}

	return locks
}

func (volume *sharedVolume) getLock(host string) (*volumeLock, error) {

	lockFile := volume.GetLockFileFor(host)
	if contents, err := ioutil.ReadFile(lockFile); err == nil {

		lockTimestamp := string(contents)
		if lockTime, err := time.Parse(time.RFC3339, lockTimestamp); err == nil {

			lock := &volumeLock{
				volume:       volume,
				lockFilename: lockFile,
				hostname:     host,
				lockedTime:   lockTime,
			}
			return lock, nil
		}
		return nil, err

	} else if os.IsNotExist(err) {
		return nil, nil
	} else {
		return nil, err
	}
}

// Locks the volume
func (volume *sharedVolume) lock() error {

	lockFilename := volume.GetLockFile()

	now := time.Now().UTC().Format(time.RFC3339)

	file, err := os.OpenFile(lockFilename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err == nil {

		length, err := file.WriteString(now)
		if err == nil && length != len(now) {
			err = fmt.Errorf("Failed to write timestamp to lockfile")
		}

		file.Close()

		return err

	}

	return err
}

// Unlocks the volume
func (volume *sharedVolume) unlock() error {
	var err error

	lockFilename := volume.GetLockFile()

	if _, err = os.Stat(volume.Mountpoint); os.IsNotExist(err) {
		return nil
	}

	if _, err = os.Stat(lockFilename); err == nil {
		err = os.Remove(lockFilename)
	}

	return err
}
