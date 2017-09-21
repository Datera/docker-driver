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
	DefaultFS          = "ext4"
	DefaultReplicas    = 3
	DefaultPlacement   = "hybrid"
	DefaultPersistence = "manual"
	DriverVersion      = "1.0.8"
	// Driver Version History
	// 1.0.3 -- Major revamp to become /v2 docker plugin framework compatible
	// 1.0.4 -- Adding QoS and PlacementMode volume options
	// 1.0.5 -- Added stateful DB connection tracking.  Small fixes
	// 1.0.6 -- Adding support for Docker under DCOS
	// 1.0.7 -- Adding template support, fixed ACL creation bug, changed initiator prefix
	// 1.0.8 -- Adding persistence mode volume option and bugfixes, updated makefile with 'make linux' option
	//          added helper DB methods, updated logging and DCOS implicit create support

	DRIVER = "Docker-Volume"

	// Volume Options
	OptSize        = "size"
	OptReplica     = "replica"
	OptTemplate    = "template"
	OptFstype      = "fsType"
	OptMaxiops     = "maxIops"
	OptMaxbw       = "maxBW"
	OptPlacement   = "placementMode"
	OptPersistence = "persistenceMode"

	// V2 Volume Plugin static mounts must be under /mnt
	MountLoc       = "/mnt"
	EnvFwk         = "DATERA_FRAMEWORK"
	EnvSize        = "DATERA_VOL_SIZE"
	EnvReplica     = "DATERA_REPLICAS"
	EnvPlacement   = "DATERA_PLACEMENT"
	EnvMaxiops     = "DATERA_MAX_IOPS"
	EnvMaxbw       = "DATERA_MAX_BW"
	EnvTemplate    = "DATERA_TEMPLATE"
	EnvFstype      = "DATERA_FSTYPE"
	EnvPersistence = "DATERA_PERSISTENCE"
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
		log.Debugf("Found already created volume: %s", r.Name)
		return nil
	}
	// Quick hack to check if api didn't find a volume
	if err != nil && !strings.Contains(err.Error(), "not exist") {
		return err
	}
	log.Debugf("Creating Volume: %s", r.Name)

	size, _ := strconv.ParseUint(volOpts[OptSize], 10, 64)
	replica, _ := strconv.ParseUint(volOpts[OptReplica], 10, 8)
	template := volOpts[OptTemplate]
	fsType := volOpts[OptFstype]
	maxIops, _ := strconv.ParseUint(volOpts[OptMaxiops], 10, 64)
	maxBW, _ := strconv.ParseUint(volOpts[OptMaxbw], 10, 64)
	placementMode, _ := volOpts[OptPlacement]
	persistence, _ := volOpts[OptPersistence]

	// Set values from environment variables if we're running inside
	// DCOS.  This is only needed if running under Docker.  Running under
	// Mesos unified containers allows passing these in normally
	if fwk := os.Getenv(EnvFwk); fwk == "dcos" || fwk == "DCOS" {
		setFromEnvs(&size, &replica, &fsType, &maxIops, &maxBW, &placementMode, &template, &persistence)
	}

	setDefaults(&fsType, &replica, &placementMode, &persistence)

	d.Volumes[m] = UpsertVolObj(r.Name, fsType, 0, persistence)

	volObj, ok := d.Volumes[m]
	log.Debugf("volObj: %s, ok: %d", volObj, ok)

	err = d.DateraClient.CreateVolume(r.Name, int(size), int(replica), template, int(maxIops), int(maxBW), placementMode)
	if err != nil {
		return err
	}
	return nil
}

func (d DateraDriver) Remove(r *dv.RemoveRequest) error {
	log.Debugf("DateraDriver.%s", "Remove")
	log.Debugf("Removing volume %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)

	log.Debugf("Remove: mountpoint %s", m)
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
	log.Debugf("DateraDriver.%s", "List")
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
		log.Debugf("In memory volume object found for: %s", r.Name)
		// Check to see if we're running under DCOS
		if fwk := os.Getenv(EnvFwk); fwk == "dcos" || fwk == "DCOS" {
			// Check to see if we have a volume with this name
			if e, _ := d.DateraClient.VolumeExist(r.Name); !e {
				// Implicitly create volume if we don't have it
				log.Debugf("Doing implicit create of volume: %s", r.Name)
				if err := doImplicitCreate(d, r.Name, m); err != nil {
					log.Debugf("Mounting implict volume: %s", r.Name)
					vol, err := doMountStuff(d, r.Name)
					log.Error(err)
					return &dv.GetResponse{Volume: &dv.Volume{Name: vol.Name, Mountpoint: d.MountPoint(vol.Name), Status: st}}, nil
				}
			}
		}
		return &dv.GetResponse{Volume: &dv.Volume{Name: s.Name, Mountpoint: d.MountPoint(s.Name), Status: st}}, nil
	} else if e, _ := d.DateraClient.VolumeExist(r.Name); e {
		// Handle case where volume exists on Datera array, but we
		// don't have record of it here (eg it was created by another volume
		// plugin
		vol, err := doMountStuff(d, r.Name)
		if err != nil {
			log.Error(err)
		}
		return &dv.GetResponse{Volume: &dv.Volume{Name: vol.Name, Mountpoint: d.MountPoint(vol.Name), Status: st}}, nil
	} else if fwk := os.Getenv(EnvFwk); fwk == "dcos" || fwk == "DCOS" {
		if e, _ := d.DateraClient.VolumeExist(r.Name); !e {
			log.Debugf("No in-memory volume object found for: %s, doing implicit create", r.Name)
			err := doImplicitCreate(d, r.Name, m)
			if err != nil {
				log.Error(err)
			}
		}
		log.Debugf("Mounting implict volume: %s", r.Name)
		vol, err := doMountStuff(d, r.Name)
		if err != nil {
			log.Error(err)
		}
		return &dv.GetResponse{Volume: &dv.Volume{Name: vol.Name, Mountpoint: d.MountPoint(vol.Name), Status: st}}, nil
	} else {
		return &dv.GetResponse{}, fmt.Errorf("Unable to find volume mounted on %s", m)
	}
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

	d.Volumes[m] = UpsertVolObj(r.Name, vol.Filesystem, 1, vol.Persistence)

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
		if vol.Connections == 1 || vol.Persistence == "auto-delete" {
			if err := d.DateraClient.UnmountVolume(r.Name, m); err != nil {
				return err
			}
		}
		if vol.Persistence == "auto-delete" {
			log.Debugf("Volume %s persistence mode is set to: %s, deleting volume after unmount", r.Name, vol.Persistence)
			d.DateraClient.DeleteVolume(r.Name, m)
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

func setDefaults(fsType *string, replica *uint64, placementMode, persistence *string) {
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
		log.Debugf("Using default placement value of %s", DefaultPlacement)
		*placementMode = DefaultPlacement
	}
	// Set persistence to "manual"
	if *persistence == "" {
		log.Debugf("Using default persistence value of %s", DefaultPersistence)
	}
	log.Debugf("Setting defaults: fsType %s, replica %d, placementMode %s, persistenceMode %s", *fsType, *replica, *placementMode, *persistence)
}

func setFromEnvs(size, replica *uint64, fsType *string, maxIops, maxBW *uint64, placementMode, template, persistence *string) {
	*size, _ = strconv.ParseUint(os.Getenv(EnvSize), 10, 64)
	if *size == 0 {
		// We should assume this because other drivers such as REXRAY
		// default to this behavior for implicit volumes
		*size = 16
	}
	*replica, _ = strconv.ParseUint(os.Getenv(EnvReplica), 10, 8)
	*fsType = os.Getenv(EnvFstype)
	*maxIops, _ = strconv.ParseUint(os.Getenv(EnvMaxiops), 10, 64)
	*maxBW, _ = strconv.ParseUint(os.Getenv(EnvMaxbw), 10, 64)
	*placementMode = os.Getenv(EnvPlacement)
	*template = os.Getenv(EnvTemplate)
	*persistence = os.Getenv(EnvPersistence)
	log.Debugf("Reading values from Environment variables: size %d, replica %d, fsType %s, maxIops %d, maxBW %d, placementMode %s, template %s",
		*size, *replica, *fsType, *maxIops, *maxBW, *placementMode, *template)
}

func doImplicitCreate(d DateraDriver, name, m string) error {
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
		persistence   string
	)
	setFromEnvs(&size, &replica, &fsType, &maxIops, &maxBW, &placementMode, &template, &persistence)
	setDefaults(&fsType, &replica, &placementMode, &persistence)

	d.Volumes[m] = UpsertVolObj(name, fsType, 0, persistence)

	volObj, ok := d.Volumes[m]
	log.Debugf("volObj: %s, ok: %d", volObj, ok)

	log.Debugf("Sending DCOS IMPLICIT create-volume to datera server.")
	err := d.DateraClient.CreateVolume(name, int(size), int(replica), template, int(maxIops), int(maxBW), placementMode)
	if err != nil {
		return err
	}
	return nil
}

func doMountStuff(d DateraDriver, name string) (*VolObj, error) {
	m := d.MountPoint(name)
	diskPath, err := d.DateraClient.LoginVolume(name, m)
	if err != nil {
		log.Debugf("Couldn't find volume, error: %s", err)
		return nil, err
	}
	fs, _ := d.DateraClient.FindDeviceFsType(diskPath)
	// The device was previously created, but there is no filesystem
	// so we're going to use the default
	if fs == "" {
		fs = DefaultFS
	}
	// Our persistence mode is 'manual' because if the driver didn't already
	// know about it, that implies it existed previously and shouldn't be
	// removed on unmount
	vol := UpsertVolObj(name, fs, 0, "manual")
	d.Volumes[m] = vol
	return vol, err
}
