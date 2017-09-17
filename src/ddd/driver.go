package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	dv "github.com/docker/go-plugins-helpers/volume"
	log "github.com/sirupsen/logrus"
)

const (
	DefaultFS        = "ext4"
	DefaultReplicas  = 3
	DefaultPlacement = "hybrid"
	DriverVersion    = "1.0.7"
	// Driver Version History
	// 1.0.3 -- Major revamp to become /v2 docker plugin framework compatible
	// 1.0.4 -- Adding QoS and PlacementMode volume options
	// 1.0.5 -- Added stateful DB connection tracking.  Small fixes
	// 1.0.6 -- Adding support for Docker under DCOS
	// 1.0.7 -- Adding template support, fixed ACL creation bug, changed initiator prefix

	DRIVER = "Docker-Volume"

	// V2 Volume Plugin static mounts must be under /mnt
	MountLoc        = "/mnt"
	FwkEnvVar       = "DATERA_FRAMEWORK"
	SizeEnvVar      = "DATERA_VOL_SIZE"
	ReplicaEnvVar   = "DATERA_REPLICAS"
	PlacementEnvVar = "DATERA_PLACEMENT"
	MaxIopsEnvVar   = "DATERA_MAX_IOPS"
	MaxBWEnvVar     = "DATERA_MAX_BW"
	TemplateEnvVar  = "DATERA_TEMPLATE"
	FsTypeEnvVar    = "DATERA_FSTYPE"
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
	CreateVolume(string, int, int, string, int, int, string) error
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
	Volumes      map[string]*VolObj
	Mutex        *sync.Mutex
	Version      string
	Debug        bool
	Ssl          bool
}

func NewDateraDriver(restAddress, username, password, tenant string, debug, noSsl bool) DateraDriver {
	d := DateraDriver{
		Volumes: map[string]*VolObj{},
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
//  fsType -- Default: ext4
//  maxIops
//  maxBW
//  placementMode -- Default: hybrid
func (d DateraDriver) Create(r *dv.CreateRequest) error {
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
		return nil
	}
	// Quick hack to check if api didn't find a volume
	if err != nil && !strings.Contains(err.Error(), "not exist") {
		return err
	}
	log.Debugf("Creating Volume: %s", r.Name)

	size, _ := strconv.ParseUint(volOpts["size"], 10, 64)
	replica, _ := strconv.ParseUint(volOpts["replica"], 10, 8)
	template := volOpts["template"]
	fsType := volOpts["fsType"]
	maxIops, _ := strconv.ParseUint(volOpts["maxIops"], 10, 64)
	maxBW, _ := strconv.ParseUint(volOpts["maxBW"], 10, 64)
	placementMode, _ := volOpts["placementMode"]

	// Set values from environment variables if we're running inside
	// DCOS.  This is only needed if running under Docker.  Running under
	// Mesos unified containers allows passing these in normally
	if fwk := os.Getenv(FwkEnvVar); fwk == "dcos" || fwk == "DCOS" {
		setFromEnvs(&size, &replica, &fsType, &maxIops, &maxBW, &placementMode, &template)
	}

	setDefaults(&fsType, &replica, &placementMode)

	d.Volumes[m] = UpsertVolObj(r.Name, fsType, 0)

	volObj, ok := d.Volumes[m]
	log.Debugf("volObj: %s, ok: %d", volObj, ok)

	err = d.DateraClient.CreateVolume(r.Name, int(size), int(replica), template, int(maxIops), int(maxBW), placementMode)
	if err != nil {
		return err
	}
	return nil
}

func (d DateraDriver) Remove(r *dv.RemoveRequest) error {
	log.Debugf("DateraDriver.%#v", "Remove")
	log.Debugf("Removing volume %#v\n", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)

	log.Debugf("Remove: mountpoint %#v", m)
	if vol, ok := d.Volumes[m]; ok {
		log.Debugf("Remove connection count: %d", vol.Connections)
		if vol.Connections <= 1 {
			if err := d.DateraClient.UnmountVolume(r.Name, m); err != nil {
				log.Warningf("Error unmounting volume: %s", err)
			}
			vol.DelConnection()
			if err := d.DateraClient.DeleteVolume(r.Name, m); err != nil {
				// Don't return an error if we fail to delete the volume
				// this provides a better user experience.  Log the error
				// so it can be debugged if needed
				log.Warningf("Error deleting volume: %s", err)
				return nil
			}
			delete(d.Volumes, m)
		}
	}
	return nil
}

func (d DateraDriver) List() (*dv.ListResponse, error) {
	log.Debugf("DateraDriver.%#v", "List")
	log.Debugf("Listing volumes: \n")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	var vols []*dv.Volume
	for _, v := range d.Volumes {
		log.Debugf("Volume Name: %s mount-point:  %s", v.Name, d.MountPoint(v.Name))
		vols = append(vols, &dv.Volume{Name: v.Name, Mountpoint: d.MountPoint(v.Name)})
	}
	return &dv.ListResponse{Volumes: vols}, nil
}

func (d DateraDriver) Get(r *dv.GetRequest) (*dv.GetResponse, error) {
	log.Debugf("DateraDriver.%s", "Get")
	log.Debugf("Get volumes: %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	st := make(map[string]interface{})
	if s, ok := d.Volumes[m]; ok {
		return &dv.GetResponse{Volume: &dv.Volume{Name: s.Name, Mountpoint: d.MountPoint(s.Name), Status: st}}, nil
	}
	// Handle case where volume exists on Datera array, but we
	// don't have record of it here (eg it was created by another volume
	// plugin
	if e, _ := d.DateraClient.VolumeExist(r.Name); e == true {
		m := d.MountPoint(r.Name)
		diskPath, err := d.DateraClient.LoginVolume(r.Name, m)
		if err != nil {
			log.Debugf("Couldn't find volume, error: %s", err)
			return &dv.GetResponse{}, err
		}
		fs, _ := d.DateraClient.FindDeviceFsType(diskPath)
		// The device was previously created, but there is no filesystem
		// so we're going to use the default
		if fs == "" {
			fs = DefaultFS
		}
		vol := UpsertVolObj(r.Name, fs, 0)
		d.Volumes[m] = vol
		return &dv.GetResponse{Volume: &dv.Volume{Name: vol.Name, Mountpoint: d.MountPoint(vol.Name), Status: st}}, nil
	} else if fwk := os.Getenv(FwkEnvVar); fwk == "dcos" || fwk == "DCOS" {
		// We need to create this implicitly since DCOS doesn't support full
		// volume lifecycle management via Docker
		var (
			size          uint64
			replica       uint64
			fsType        string
			maxIops       uint64
			maxBW         uint64
			placementMode string
			template      string
		)
		setFromEnvs(&size, &replica, &fsType, &maxIops, &maxBW, &placementMode, &template)
		setDefaults(&fsType, &replica, &placementMode)

		d.Volumes[m] = UpsertVolObj(r.Name, fsType, 0)

		volObj, ok := d.Volumes[m]
		log.Debugf("volObj: %s, ok: %d", volObj, ok)

		log.Debugf("Sending DCOS IMPLICIT create-volume to datera server.")
		err := d.DateraClient.CreateVolume(r.Name, int(size), int(replica), template, int(maxIops), int(maxBW), placementMode)
		if err != nil {
			return &dv.GetResponse{}, err
		}
	}
	return &dv.GetResponse{}, fmt.Errorf("Unable to find volume mounted on %#v", m)
}

func (d DateraDriver) Path(r *dv.PathRequest) (*dv.PathResponse, error) {
	log.Debugf("DateraDriver.%s", "Path")
	return &dv.PathResponse{Mountpoint: d.MountPoint(r.Name)}, nil
}

func (d DateraDriver) Mount(r *dv.MountRequest) (*dv.MountResponse, error) {
	var diskPath string
	var err error
	log.Debugf("DateraDriver.%s", "Mount")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	log.Debugf("Mounting volume %s on %s\n", r.Name, m)

	vol, ok := d.Volumes[m]

	if !ok {
		return &dv.MountResponse{}, fmt.Errorf("Volume not found: %s", m)
	}

	if ok && vol.Connections > 0 {
		log.Debugf("Connections found: %d. Adding new connection", vol.Connections)
		vol.AddConnection()
		return &dv.MountResponse{Mountpoint: m}, nil
	}

	if diskPath, err = d.DateraClient.LoginVolume(r.Name, m); err != nil {
		return &dv.MountResponse{}, err
	}
	if err = d.DateraClient.MountVolume(r.Name, m, vol.Filesystem, diskPath); err != nil {
		return &dv.MountResponse{}, err
	}

	d.Volumes[m] = UpsertVolObj(r.Name, vol.Filesystem, 1)

	return &dv.MountResponse{Mountpoint: m}, nil
}

func (d DateraDriver) Unmount(r *dv.UnmountRequest) error {
	log.Debugf("DateraDriver.%s", "Unmount")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	log.Debugf("Driver::Unmount: unmounting volume %s from %s\n", r.Name, m)

	if vol, ok := d.Volumes[m]; ok {
		log.Debugf("Current Connections: %d", vol.Connections)
		if vol.Connections == 1 {
			if err := d.DateraClient.UnmountVolume(r.Name, m); err != nil {
				return err
			}
		}
		vol.DelConnection()
	} else {
		vol.ResetConnections()
		return fmt.Errorf("Unable to find volume mounted on %s", m)
	}

	return nil
}

func (d DateraDriver) Capabilities() *dv.CapabilitiesResponse {
	log.Debugf("DateraDriver.%s", "Capabilities")
	// This driver is global scope since created volumes are not bound to the
	// engine that created them.
	return &dv.CapabilitiesResponse{Capabilities: dv.Capability{Scope: "global"}}
}

func (d DateraDriver) MountPoint(name string) string {
	return filepath.Join(MountLoc, name)
}

func setDefaults(fsType *string, replica *uint64, placementMode *string) {
	// Set default filesystem to ext4
	if len(*fsType) == 0 {
		log.Debugf(
			"Using default filesystem value of %s", DefaultFS)
		*fsType = DefaultFS
	}

	// Set default replicas to 3
	if *replica == 0 {
		log.Debugf("Using default replica value of %d", DefaultReplicas)
		*replica = uint64(DefaultReplicas)
	}
	// Set default placement to "hybrid"
	if *placementMode == "" {
		log.Debugf("Using default placement value of %d", DefaultPlacement)
		*placementMode = DefaultPlacement
	}
	log.Debugf("Setting defaults: fsType %s, replica %d, placementMode %s", fsType, replica, placementMode)
}

func setFromEnvs(size, replica *uint64, fsType *string, maxIops, maxBW *uint64, placementMode, template *string) {
	*size, _ = strconv.ParseUint(os.Getenv(SizeEnvVar), 10, 64)
	if *size == 0 {
		// We should assume this because other drivers such as REXRAY
		// default to this behavior for implicit volumes
		*size = 16
	}
	*replica, _ = strconv.ParseUint(os.Getenv(ReplicaEnvVar), 10, 8)
	*fsType = os.Getenv(FsTypeEnvVar)
	*maxIops, _ = strconv.ParseUint(os.Getenv(MaxIopsEnvVar), 10, 64)
	*maxBW, _ = strconv.ParseUint(os.Getenv(MaxBWEnvVar), 10, 64)
	*placementMode = os.Getenv(PlacementEnvVar)
	*template = os.Getenv(TemplateEnvVar)
	log.Debugf("Reading values from Environment variables: size %d, replica %d, fsType %s, maxIops %d, maxBW %d, placementMode %s, template %s",
		*size, *replica, *fsType, *maxIops, *maxBW, *placementMode, *template)
}
