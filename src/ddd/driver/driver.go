package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	dc "ddd/client"
	co "ddd/common"
	dv "github.com/docker/go-plugins-helpers/volume"
	log "github.com/sirupsen/logrus"
)

const (
	DefaultSize        = 16
	DefaultFS          = "ext4"
	DefaultReplicas    = 3
	DefaultPlacement   = "hybrid"
	DefaultPersistence = "manual"
	DriverVersion      = "1.1.1"
	// Driver Version History
	// 1.0.3 -- Major revamp to become /v2 docker plugin framework compatible
	// 1.0.4 -- Adding QoS and PlacementMode volume options
	// 1.0.5 -- Added stateful DB connection tracking.  Small fixes
	// 1.0.6 -- Adding support for Docker under DCOS
	// 1.0.7 -- Adding template support, fixed ACL creation bug, changed initiator prefix
	// 1.0.8 -- Adding persistence mode volume option and bugfixes, updated makefile with 'make linux' option
	//          added helper DB methods, updated logging and DCOS implicit create support
	// 1.1.0 -- Reorg into subpackages.  Bugfixes and implicit creation logic changes
	// 1.1.1 -- Multipathing update.  Switched iscsi login discover back to using /by-uuid/

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
	// TODO(mss): add a clone_src opt so we can specify a clone rather than a
	// new volume creation
	OptCloneSrc = "cloneSrcNotUsedYet"

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

	// Misc
	DeleteConst = "auto"
)

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
	GetIQNandPortals(string) (string, []string, string, error)
	FindDeviceFsType(string) (string, error)
}

type DateraDriver struct {
	DateraClient IClient
	Volumes      map[string]*co.VolObj
	Mutex        *sync.Mutex
	Version      string
	Debug        bool
	Ssl          bool
}

func NewDateraDriver(restAddress, username, password, tenant string, debug, noSsl bool) DateraDriver {
	d := DateraDriver{
		Volumes: map[string]*co.VolObj{},
		Mutex:   &sync.Mutex{},
		Version: DriverVersion,
		Debug:   debug,
	}
	log.Debugf(
		"Creating DateraClient object with restAddress: %s", restAddress)
	client := dc.NewClient(restAddress, username, password, tenant, debug, !noSsl, DRIVER, DriverVersion)
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
	if isDcosDocker() {
		setFromEnvs(&size, &replica, &fsType, &maxIops, &maxBW, &placementMode, &template, &persistence)
	}

	setDefaults(&size, &fsType, &replica, &placementMode, &persistence)

	d.Volumes[m] = co.UpsertVolObj(r.Name, fsType, 0, persistence)

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
	var (
		vol *co.VolObj
		err error
		ok  bool
	)
	if vol, ok = d.Volumes[m]; ok {
		log.Debugf("In memory volume object found for: %s", r.Name)
		// Check to see if we're running under DCOS
		if isDcosDocker() {
			// Check to see if we have a volume with this name
			if e, _ := d.DateraClient.VolumeExist(r.Name); !e {
				// Implicitly create volume if we don't have it
				log.Debugf("Doing implicit create of volume: %s", r.Name)
				if vol, err = doImplicitCreate(d, r.Name, m); err != nil {
					return &dv.GetResponse{}, err
				}
			}
		}
	} else if e, _ := d.DateraClient.VolumeExist(r.Name); e {
		// Handle case where volume exists on Datera array, but we
		// don't have record of it here (eg it was created by another volume
		// plugin.  We assume manual lifecycle here
		vol, err = doMount(d, r.Name, "manual", "")
		if err != nil {
			log.Error(err)
			return &dv.GetResponse{}, err
		}
	} else if isDcosDocker() {
		if e, _ := d.DateraClient.VolumeExist(r.Name); !e {
			log.Debugf("No in-memory volume object found for: %s, doing implicit create", r.Name)
			vol, err = doImplicitCreate(d, r.Name, m)
			if err != nil {
				log.Error(err)
				return &dv.GetResponse{}, err
			}
		}
	} else {
		return &dv.GetResponse{}, fmt.Errorf("Unable to find volume mounted on %s", m)
	}
	return &dv.GetResponse{Volume: &dv.Volume{Name: vol.Name, Mountpoint: d.MountPoint(vol.Name), Status: st}}, nil
}

func (d DateraDriver) Path(r *dv.PathRequest) (*dv.PathResponse, error) {
	log.Debugf("DateraDriver.%s", "Path")
	return &dv.PathResponse{Mountpoint: d.MountPoint(r.Name)}, nil
}

func (d DateraDriver) Mount(r *dv.MountRequest) (*dv.MountResponse, error) {
	log.Debugf("DateraDriver.%s", "Mount")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	log.Debugf("Mounting volume %s on %s\n", r.Name, m)

	vol, ok := d.Volumes[m]

	if !ok {
		return &dv.MountResponse{}, fmt.Errorf("Volume not found: %s", m)
	}

	if vol.Persistence == "" {
		vol.Persistence = "manual"
	}

	_, err := doMount(d, r.Name, vol.Persistence, vol.Filesystem)
	if err != nil {
		return &dv.MountResponse{}, err
	}
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
		if vol.Connections <= 1 || vol.Persistence == DeleteConst {
			if err := d.DateraClient.UnmountVolume(r.Name, m); err != nil {
				log.Errorf("Unmount Error: %s", err)
			}
		}
		if vol.Persistence == DeleteConst {
			log.Debugf("Volume %s persistence mode is set to: %s, deleting volume after unmount", r.Name, vol.Persistence)
			if err := d.DateraClient.DeleteVolume(r.Name, m); err != nil {
				log.Errorf("Deletion Error: %s", err)
				delete(d.Volumes, m)
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

func setDefaults(size *uint64, fsType *string, replica *uint64, placementMode, persistence *string) {
	if *size == 0 {
		log.Debugf(
			"Using default size value of %s", DefaultSize)
		*size = DefaultSize
	}
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
	log.Debugf("After setting defaults: size %s, fsType %s, replica %d, placementMode %s, persistenceMode %s", *size, *fsType, *replica, *placementMode, *persistence)
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

func doImplicitCreate(d DateraDriver, name, m string) (*co.VolObj, error) {
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
	setDefaults(&size, &fsType, &replica, &placementMode, &persistence)

	d.Volumes[m] = co.UpsertVolObj(name, fsType, 0, persistence)

	volObj, ok := d.Volumes[m]
	log.Debugf("volObj: %s, ok: %d", volObj, ok)

	log.Debugf("Sending DCOS IMPLICIT create-volume to datera server.")
	err := d.DateraClient.CreateVolume(name, int(size), int(replica), template, int(maxIops), int(maxBW), placementMode)
	if err != nil {
		return nil, err
	}
	log.Debugf("Mounting implict volume: %s", name)
	vol, err := doMount(d, name, persistence, fsType)
	return vol, err
}

func doMount(d DateraDriver, name, pmode, fs string) (*co.VolObj, error) {
	m := d.MountPoint(name)
	diskPath, err := d.DateraClient.LoginVolume(name, m)
	if err != nil {
		log.Debugf("Couldn't find volume, error: %s", err)
		return nil, err
	}
	newfs, _ := d.DateraClient.FindDeviceFsType(diskPath)
	newfs = strings.TrimSpace(newfs)
	fs = strings.TrimSpace(fs)
	// The device was previously created, but there is no filesystem
	// so we're going to use the default
	if newfs == "" && fs == "" {
		log.Debugf("Couldn't detect fs and parameter fs is not set for volume: %s", name)
		fs = DefaultFS
	} else if fs == "" && newfs != "" {
		log.Debugf("Setting volume: %s fs to detected type: %s", name, newfs)
		fs = newfs
	}
	vol := co.UpsertVolObj(name, fs, 0, pmode)
	if err = d.DateraClient.MountVolume(name, m, fs, diskPath); err != nil {
		return vol, err
	}
	d.Volumes[m] = vol
	return vol, err
}

func isDcosDocker() bool {
	fwk := strings.ToLower(os.Getenv(EnvFwk))
	if fwk == "dcos-docker" {
		return true
	}
	return false
}

func isDcosMesos() bool {
	fwk := strings.ToLower(os.Getenv(EnvFwk))
	if fwk == "dcos-mesos" {
		return true
	}
	return false
}
