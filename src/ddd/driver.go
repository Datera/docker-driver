package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	log "github.com/Sirupsen/logrus"
	dv "github.com/docker/go-plugins-helpers/volume"
)

const (
	DefaultFS       = "ext4"
	DefaultReplicas = 3
	DriverVersion   = "1.0.3"
	DRIVER          = "Docker-Volume"

	// V2 Volume Plugin static mounts must be under /mnt
	MountLoc = "/mnt"
)

type VolumeEntry struct {
	Name        string
	FsType      string
	Connections int
}

// Need to require interface instead of DateraClient directly
// so we can mock DateraClient out more easily
type IClient interface {
	VolumeExist(string) (bool, error)
	CreateVolume(string, int, int, string, int, int) error
	DeleteVolume(string, string) error
	LoginVolume(string, string) (string, error)
	MountVolume(string, string, string, string) error
	UnmountVolume(string, string) error
	DetachVolume(string) error
	GetIQNandPortal(string) (string, string, string, error)
	FindDeviceFsType(string) (string, error)
}

type DateraDriver struct {
	DateraClient IClient
	Volumes      map[string]*VolumeEntry
	Mutex        *sync.Mutex
	Version      string
	Debug        bool
	Ssl          bool
}

func NewDateraDriver(restAddress, username, password, tenant string, debug, noSsl bool) DateraDriver {
	d := DateraDriver{
		Volumes: map[string]*VolumeEntry{},
		Mutex:   &sync.Mutex{},
		Version: DriverVersion,
		Debug:   debug,
	}
	log.Debugf(
		"Creating DateraClient object with restAddress: %s", restAddress)
	client := NewClient(restAddress, username, password, tenant, debug, !noSsl, DRIVER, DriverVersion)
	d.DateraClient = client
	log.Debugf("DateraDriver: %#v", d)
	log.Debugf("Driver Version: %s", d.Version)
	return d
}

// Create creates a volume on the configured Datera backend
//
// Specified using `--opt key=value` in the docker volume create command
//
// Available Options:
//	size
//	replica -- Default: 3
//  template
//  FsType -- Default: ext4
//  maxIops
//  maxBW
func (d DateraDriver) Create(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%s", "Create")
	log.Debugf("Creating volume %s\n", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	log.Debugf("Mountpoint for Request %s is %s", r.Name, m)
	volOpts := r.Options
	log.Debugf("Volume Options: %#v", volOpts)

	log.Debugf("Checking for existing volume: %s", r.Name)
	exist, err := d.DateraClient.VolumeExist(r.Name)
	if err == nil && exist {
		log.Debugf("Found already created volume: ", r.Name)
		return dv.Response{}
	}
	// Quick hack to check if api didn't find a volume
	if err != nil && !strings.Contains(err.Error(), "not exist") {
		return dv.Response{Err: err.Error()}
	}
	log.Debugf("Creating Volume: %s", r.Name)

	size, _ := strconv.ParseUint(volOpts["size"], 10, 64)
	replica, _ := strconv.ParseUint(volOpts["replica"], 10, 8)
	template := volOpts["template"]
	FsType := volOpts["FsType"]
	maxIops, _ := strconv.ParseUint(volOpts["maxIops"], 10, 64)
	maxBW, _ := strconv.ParseUint(volOpts["maxBW"], 10, 64)

	// Set default filesystem to ext4
	if len(FsType) == 0 {
		log.Debugf(
			"Using default filesystem value of %s", DefaultReplicas)
		FsType = DefaultFS
	}

	// Set default replicas to 3
	if replica == 0 {
		log.Debugf("Using default replica value of %d", DefaultReplicas)
		replica = DefaultReplicas
	}

	d.Volumes[m] = &VolumeEntry{Name: r.Name, FsType: FsType, Connections: 0}

	volEntry, ok := d.Volumes[m]
	log.Debugf("volEntry: %s, ok: %d", volEntry, ok)

	log.Debugf("Sending create-volume to datera server.")
	err = d.DateraClient.CreateVolume(r.Name, int(size), int(replica), template, int(maxIops), int(maxBW))
	if err != nil {
		return dv.Response{Err: err.Error()}
	}
	return dv.Response{}
}

func (d DateraDriver) Remove(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%#v", "Remove")
	log.Debugf("Removing volume %#v\n", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)

	log.Debugf("Remove: mountpoint %#v", m)
	if s, ok := d.Volumes[m]; ok {
		log.Debugf("Remove: conection count ", s.Connections)
		if s.Connections <= 1 {
			if d.DateraClient != nil {
				if err := d.DateraClient.DeleteVolume(r.Name, m); err != nil {
					// Don't return an error if we fail to delete the volume
					// this provides a better user experience.  Log the error
					// so it can be debugged if needed
					log.Warningf("Error deleting volume: %s", err)
					return dv.Response{}
				}
			}
			delete(d.Volumes, m)
		}
	}
	return dv.Response{}
}

func (d DateraDriver) List(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%#v", "List")
	log.Debugf("Listing volumes: \n")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	var vols []*dv.Volume
	for _, v := range d.Volumes {
		log.Debugf("Volume Name: %s mount-point:  %s", v.Name, d.MountPoint(v.Name))
		vols = append(vols, &dv.Volume{Name: v.Name, Mountpoint: d.MountPoint(v.Name)})
	}
	return dv.Response{Volumes: vols}
}

func (d DateraDriver) Get(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%s", "Get")
	log.Debugf("Get volumes: %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	st := make(map[string]interface{})
	if s, ok := d.Volumes[m]; ok {
		return dv.Response{Volume: &dv.Volume{Name: s.Name, Mountpoint: d.MountPoint(s.Name), Status: st}}
	}
	// Handle case where volume exists on Datera array, but we
	// don't have record of it here (eg it was created by another volume
	// plugin
	if e, _ := d.DateraClient.VolumeExist(r.Name); e == true {
		m := d.MountPoint(r.Name)
		volUUID, err := d.DateraClient.LoginVolume(r.Name, m)
		if err != nil {
			log.Debugf("Couldn't find volume, error: %s", err)
			return dv.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", m)}
		}
		fs, _ := d.DateraClient.FindDeviceFsType(volUUID)
		// The device was previously created, but there is no filesystem
		// so we're going to use the default
		if fs == "" {
			fs = DefaultFS
		}
		s := &VolumeEntry{Name: r.Name, FsType: fs, Connections: 0}
		d.Volumes[m] = s
		return dv.Response{Volume: &dv.Volume{Name: s.Name, Mountpoint: d.MountPoint(s.Name), Status: st}}
	}
	return dv.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", m)}
}

func (d DateraDriver) Path(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%s", "Path")
	return dv.Response{Mountpoint: d.MountPoint(r.Name)}
}

func (d DateraDriver) Mount(r dv.MountRequest) dv.Response {
	var volUUID string
	var err error
	log.Debugf("DateraDriver.%s", "Mount")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	log.Debugf("Mounting volume %s on %#v\n", r.Name, m)

	s, ok := d.Volumes[m]

	if !ok {
		return dv.Response{Err: fmt.Sprintf("Volume not found: %s", m)}
	}

	if ok && s.Connections > 0 {
		s.Connections++
		return dv.Response{Mountpoint: m}
	}

	if volUUID, err = d.DateraClient.LoginVolume(r.Name, m); err != nil {
		return dv.Response{Err: err.Error()}
	}
	if err = d.DateraClient.MountVolume(r.Name, m, s.FsType, volUUID); err != nil {
		return dv.Response{Err: err.Error()}
	}

	d.Volumes[m] = &VolumeEntry{Name: r.Name, FsType: s.FsType, Connections: 1}

	return dv.Response{Mountpoint: m}
}

func (d DateraDriver) Unmount(r dv.UnmountRequest) dv.Response {
	log.Debugf("DateraDriver.%s", "Unmount")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	log.Debugf("Driver::Unmount: unmounting volume %#v from %#v\n", r.Name, m)

	if s, ok := d.Volumes[m]; ok {
		log.Debugf("Current Connections: %s", s.Connections)
		if s.Connections == 1 {
			if err := d.DateraClient.UnmountVolume(r.Name, m); err != nil {
				return dv.Response{Err: err.Error()}
			}
		}
		s.Connections--
	} else {
		return dv.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", m)}
	}

	return dv.Response{}
}

func (d DateraDriver) Capabilities(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%#v", "Capabilities")
	// This driver is global scope since created volumes are not bound to the
	// engine that created them.
	return dv.Response{Capabilities: dv.Capability{Scope: "global"}}
}

func (d DateraDriver) MountPoint(name string) string {
	return filepath.Join(MountLoc, name)
}
