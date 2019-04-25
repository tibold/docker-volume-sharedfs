package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	log "github.com/Sirupsen/logrus"

	"github.com/docker/go-plugins-helpers/volume"
)

var (
	root             = flag.String("root", "", "Base directory where volumes are created in the cluster")
	debug            = flag.Bool("debug", true, "Enable verbose logging")
	hostname         = flag.String("hostname", "", "The hostname used in locking operations")
	lockInterval     = 20 * time.Second
	lockTimeout      = 60 * time.Second
	cleanupInterval  = 60 * time.Minute
	defaultProtected = false
	defaultExclusive = false
)

func main() {
	parseEnvironment()
	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	if *hostname == "" {
		*hostname, _ = os.Hostname()
	}

	log.Debugf("Starting with hostname=%s; root=%s", *hostname, *root)

	// userID, _ := user.Lookup("root")
	// groupID, _ := strconv.Atoi(userID.Gid)

	driver := newSharedFSDriver(*root)
	handler := volume.NewHandler(driver)
	fmt.Println(handler.ServeUnix("sharedfs", 0))
}

func parseEnvironment() {

	value := os.Getenv("SFS_DEBUG")
	if parsedBool, err := strconv.ParseBool(value); err == nil {
		*debug = parsedBool
	}

	value = os.Getenv("SFS_LOCK_INTERVAL")
	if parsedInt, err := strconv.ParseInt(value, 10, 32); err == nil {
		lockInterval = time.Duration(parsedInt) * time.Second
	}

	value = os.Getenv("SFS_LOCK_TIMEOUT")
	if parsedInt, err := strconv.ParseInt(value, 10, 32); err == nil {
		lockTimeout = time.Duration(parsedInt) * time.Second
	}

	value = os.Getenv("SFS_CLEANUP_INTERVAL")
	if parsedInt, err := strconv.ParseInt(value, 10, 32); err == nil {
		cleanupInterval = time.Duration(parsedInt) * time.Minute
	}

	value = os.Getenv("SFS_DEFAULT_PROTECTED")
	if parsedBool, err := strconv.ParseBool(value); err == nil {
		defaultProtected = parsedBool
	}

	value = os.Getenv("SFS_DEFAULT_EXCLUSIVE")
	if parsedBool, err := strconv.ParseBool(value); err == nil {
		defaultProtected = parsedBool
	}
}
