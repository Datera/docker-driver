package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	dc "ddd/client"
	co "ddd/common"
	dv "github.com/docker/go-plugins-helpers/volume"
)

const (
	DefaultSize        = 16
	DefaultFS          = "ext4"
	DefaultReplicas    = 3
	DefaultPlacement   = "hybrid"
	DefaultPersistence = "manual"
	DriverVersion      = "1.2.0"
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
	// 1.1.2 -- Moved to volume option struct interface in the client.  Fixed multipathing bug
	// 1.1.3 -- Added cloneSrc option which takes an AppInstance name and clones the created volume from it
	// 1.1.4 -- Fixed bug in non-multipath case where diskPath was not getting populated during login
	//          Added run_driver.py helper script for running the driver inside a container with
	//          log parsing.  Modified config.json for this purpose as well
	// 1.2.0 -- Switched all functions to require a context object for log
	//			tracing purposes

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
	OptCloneSrc    = "cloneSrc"

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
	EnvCloneSrc    = "DATERA_CLONE_SRC"

	// Misc
	DeleteConst = "auto"
)

// Need to require interface instead of DateraClient directly
// so we can mock DateraClient out more easily
type IClient interface {
	VolumeExist(context.Context, string) (bool, error)
	CreateVolume(context.Context, string, *dc.VolOpts) error
	DeleteVolume(context.Context, string, string) error
	LoginVolume(context.Context, string, string) (string, error)
	MountVolume(context.Context, string, string, string, string) error
	UnmountVolume(context.Context, string, string) error
	DetachVolume(context.Context, string) error
	GetIQNandPortals(context.Context, string) (string, []string, string, error)
	FindDeviceFsType(context.Context, string) (string, error)
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
	ctxt := co.MkCtxt("NewDateraDriver")
	co.Debugf(ctxt,
		"Creating DateraClient object with restAddress: %s", restAddress)
	client := dc.NewClient(ctxt, restAddress, username, password, tenant, debug, !noSsl, DRIVER, DriverVersion)
	d.DateraClient = client
	co.Debugf(ctxt, "DateraDriver: %#v", d)
	co.Debugf(ctxt, "Driver Version: %s", d.Version)
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
//  persistenceMode -- Default: manual
//  cloneSrc
func (d DateraDriver) Create(r *dv.CreateRequest) error {
	ctxt := co.MkCtxt("Create")
	co.Debugf(ctxt, "DateraDriver.Create: %#v", r)
	co.Debugf(ctxt, "Creating volume %s\n", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	co.Debugf(ctxt, "Mountpoint for Request %s is %s", r.Name, m)
	volOpts := r.Options
	co.Debugf(ctxt, "Volume Options: %#v", volOpts)

	co.Debugf(ctxt, "Checking for existing volume: %s", r.Name)
	exist, err := d.DateraClient.VolumeExist(ctxt, r.Name)
	if err == nil && exist {
		co.Debugf(ctxt, "Found already created volume: %s", r.Name)
		return nil
	}
	// Quick hack to check if api didn't find a volume
	if err != nil && !strings.Contains(err.Error(), "not exist") {
		return err
	}
	co.Debugf(ctxt, "Creating Volume: %s", r.Name)

	size, _ := strconv.ParseUint(volOpts[OptSize], 10, 64)
	replica, _ := strconv.ParseUint(volOpts[OptReplica], 10, 8)
	template := volOpts[OptTemplate]
	fsType := volOpts[OptFstype]
	maxIops, _ := strconv.ParseUint(volOpts[OptMaxiops], 10, 64)
	maxBW, _ := strconv.ParseUint(volOpts[OptMaxbw], 10, 64)
	placementMode, _ := volOpts[OptPlacement]
	persistence, _ := volOpts[OptPersistence]
	cloneSrc, _ := volOpts[OptCloneSrc]

	vOpts := dc.VolOpts{
		size,
		replica,
		template,
		fsType,
		maxIops,
		maxBW,
		placementMode,
		persistence,
		cloneSrc,
	}

	co.Debugf(ctxt, "Passed in volume opts: %s", co.Prettify(vOpts))

	// Set values from environment variables if we're running inside
	// DCOS.  This is only needed if running under Docker.  Running under
	// Mesos unified containers allows passing these in normally
	if isDcosDocker() {
		setFromEnvs(ctxt, &vOpts)
	}

	setDefaults(ctxt, &vOpts)

	d.Volumes[m] = co.UpsertVolObj(r.Name, vOpts.FsType, 0, vOpts.Persistence)

	volObj, ok := d.Volumes[m]
	co.Debugf(ctxt, "volObj: %s, ok: %d", volObj, ok)

	err = d.DateraClient.CreateVolume(ctxt, r.Name, &vOpts)
	if err != nil {
		return err
	}
	return nil
}

func (d DateraDriver) Remove(r *dv.RemoveRequest) error {
	ctxt := co.MkCtxt("Remove")
	co.Debugf(ctxt, "DateraDriver.Remove: %#v", r)
	co.Debugf(ctxt, "Removing volume %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)

	co.Debugf(ctxt, "Remove: mountpoint %s", m)
	if vol, ok := d.Volumes[m]; ok {
		co.Debugf(ctxt, "Remove connection count: %d", vol.Connections)
		if vol.Connections <= 1 {
			if err := d.DateraClient.UnmountVolume(ctxt, r.Name, m); err != nil {
				co.Warningf(ctxt, "Error unmounting volume: %s", err)
			}
			vol.DelConnection()
			if err := d.DateraClient.DeleteVolume(ctxt, r.Name, m); err != nil {
				// Don't return an error if we fail to delete the volume
				// this provides a better user experience.  Log the error
				// so it can be debugged if needed
				co.Warningf(ctxt, "Error deleting volume: %s", err)
				return nil
			}
			delete(d.Volumes, m)
		}
	}
	return nil
}

func (d DateraDriver) List() (*dv.ListResponse, error) {
	ctxt := co.MkCtxt("List")
	co.Debugf(ctxt, "DateraDriver.List")
	co.Debugf(ctxt, "Listing volumes: \n")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	var vols []*dv.Volume
	for _, v := range d.Volumes {
		co.Debugf(ctxt, "Volume Name: %s mount-point:  %s", v.Name, d.MountPoint(v.Name))
		vols = append(vols, &dv.Volume{Name: v.Name, Mountpoint: d.MountPoint(v.Name)})
	}
	return &dv.ListResponse{Volumes: vols}, nil
}

func (d DateraDriver) Get(r *dv.GetRequest) (*dv.GetResponse, error) {
	ctxt := co.MkCtxt("Get")
	co.Debugf(ctxt, "DateraDriver.Get: %#v", r)
	co.Debugf(ctxt, "Get volumes: %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	// st := make(map[string]interface{})
	var (
		vol *co.VolObj
		err error
		ok  bool
	)
	if vol, ok = d.Volumes[m]; ok {
		co.Debugf(ctxt, "In memory volume object found for: %s", r.Name)
		// Check to see if we're running under DCOS
		// Check to see if we have a volume with this name
		if e, _ := d.DateraClient.VolumeExist(ctxt, r.Name); e {
			// Implicitly create volume if we don't have it
			co.Debugf(ctxt, "Doing implicit create of volume: %s", r.Name)
			if isDcosDocker() {
				if vol, err = doImplicitCreate(ctxt, d, r.Name, m); err != nil {
					return &dv.GetResponse{}, err
				}
			}
		} else {
			msg := fmt.Sprintf("Couldn't find %s, but we have a local record, deleting local record", r.Name)
			err = fmt.Errorf(msg)
			co.Errorf(ctxt, msg)
			vol.DelConnection()
			delete(d.Volumes, m)
			return &dv.GetResponse{}, err
		}
	} else if e, _ := d.DateraClient.VolumeExist(ctxt, r.Name); e {
		// Handle case where volume exists on Datera array, but we
		// don't have record of it here (eg it was created by another volume
		// plugin.  We assume manual lifecycle here
		vol, err = doMount(ctxt, d, r.Name, "manual", "")
		if err != nil {
			co.Error(ctxt, err)
			return &dv.GetResponse{}, err
		}
	} else if isDcosDocker() {
		if e, _ := d.DateraClient.VolumeExist(ctxt, r.Name); !e {
			co.Debugf(ctxt, "No in-memory volume object found for: %s, doing implicit create", r.Name)
			vol, err = doImplicitCreate(ctxt, d, r.Name, m)
			if err != nil {
				co.Error(ctxt, err)
				return &dv.GetResponse{}, err
			}
		}
	} else {
		return &dv.GetResponse{}, fmt.Errorf("Unable to find volume mounted on %s", m)
	}
	return &dv.GetResponse{Volume: &dv.Volume{Name: vol.Name, Mountpoint: d.MountPoint(vol.Name), Status: make(map[string]interface{}, 0)}}, nil
}

func (d DateraDriver) Path(r *dv.PathRequest) (*dv.PathResponse, error) {
	ctxt := co.MkCtxt("Path")
	co.Debugf(ctxt, "DateraDriver.Path")
	return &dv.PathResponse{Mountpoint: d.MountPoint(r.Name)}, nil
}

func (d DateraDriver) Mount(r *dv.MountRequest) (*dv.MountResponse, error) {
	ctxt := co.MkCtxt("Mount")
	co.Debugf(ctxt, "DateraDriver.Mount: %#v", r)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	co.Debugf(ctxt, "Mounting volume %s on %s\n", r.Name, m)

	vol, ok := d.Volumes[m]

	if !ok {
		return &dv.MountResponse{}, fmt.Errorf("Volume not found: %s", m)
	}

	if vol.Persistence == "" {
		vol.Persistence = "manual"
	}

	_, err := doMount(ctxt, d, r.Name, vol.Persistence, vol.Filesystem)
	if err != nil {
		return &dv.MountResponse{}, err
	}
	return &dv.MountResponse{Mountpoint: m}, nil
}

func (d DateraDriver) Unmount(r *dv.UnmountRequest) error {
	ctxt := co.MkCtxt("Unmount")
	co.Debugf(ctxt, "DateraDriver.Unmount: %#v", r)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	co.Debugf(ctxt, "Driver::Unmount: unmounting volume %s from %s\n", r.Name, m)

	if vol, ok := d.Volumes[m]; ok {
		co.Debugf(ctxt, "Current Connections: %d", vol.Connections)
		if vol.Connections <= 1 || vol.Persistence == DeleteConst {
			if err := d.DateraClient.UnmountVolume(ctxt, r.Name, m); err != nil {
				co.Errorf(ctxt, "Unmount Error: %s", err)
			}
		}
		if vol.Persistence == DeleteConst {
			co.Debugf(ctxt, "Volume %s persistence mode is set to: %s, deleting volume after unmount", r.Name, vol.Persistence)
			if err := d.DateraClient.DeleteVolume(ctxt, r.Name, m); err != nil {
				co.Errorf(ctxt, "Deletion Error: %s", err)
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
	ctxt := co.MkCtxt("Capabilities")
	co.Debugf(ctxt, "DateraDriver.Capabilities")
	// This driver is global scope since created volumes are not bound to the
	// engine that created them.
	return &dv.CapabilitiesResponse{Capabilities: dv.Capability{Scope: "global"}}
}

func (d DateraDriver) MountPoint(name string) string {
	return filepath.Join(MountLoc, name)
}

func setDefaults(ctxt context.Context, volOpts *dc.VolOpts) {
	if volOpts.Size == 0 {
		co.Debugf(ctxt,
			"Using default size value of %d", DefaultSize)
		volOpts.Size = DefaultSize
	}
	// Set default filesystem to ext4
	if len(volOpts.FsType) == 0 {
		co.Debugf(ctxt,
			"Using default filesystem value of %s", DefaultFS)
		volOpts.FsType = DefaultFS
	}

	// Set default replicas to 3
	if volOpts.Replica == 0 {
		co.Debugf(ctxt, "Using default replica value of %d", DefaultReplicas)
		volOpts.Replica = DefaultReplicas
	}
	// Set default placement to "hybrid"
	if volOpts.PlacementMode == "" {
		co.Debugf(ctxt, "Using default placement value of %s", DefaultPlacement)
		volOpts.PlacementMode = DefaultPlacement
	}
	// Set persistence to "manual"
	if volOpts.Persistence == "" {
		co.Debugf(ctxt, "Using default persistence value of %s", DefaultPersistence)
		volOpts.Persistence = DefaultPersistence
	}
	co.Debugf(ctxt, "After setting defaults: size %d, fsType %s, replica %d, placementMode %s, persistenceMode %s",
		volOpts.Size, volOpts.FsType, volOpts.Replica, volOpts.PlacementMode, volOpts.Persistence)
}

func setFromEnvs(ctxt context.Context, volOpts *dc.VolOpts) {
	size, _ := strconv.ParseUint(os.Getenv(EnvSize), 10, 64)
	if size == 0 {
		// We should assume this because other drivers such as REXRAY
		// default to this behavior for implicit volumes
		size = 16
	}
	replica, _ := strconv.ParseUint(os.Getenv(EnvReplica), 10, 8)
	fsType := os.Getenv(EnvFstype)
	maxIops, _ := strconv.ParseUint(os.Getenv(EnvMaxiops), 10, 64)
	maxBW, _ := strconv.ParseUint(os.Getenv(EnvMaxbw), 10, 64)
	placementMode := os.Getenv(EnvPlacement)
	template := os.Getenv(EnvTemplate)
	persistence := os.Getenv(EnvPersistence)
	cloneSrc := os.Getenv(EnvCloneSrc)
	volOpts.Size = size
	volOpts.Replica = replica
	volOpts.Template = template
	volOpts.FsType = fsType
	volOpts.MaxIops = maxIops
	volOpts.MaxBW = maxBW
	volOpts.PlacementMode = placementMode
	volOpts.Persistence = persistence
	volOpts.CloneSrc = cloneSrc
	co.Debugf(ctxt, "Reading values from Environment variables: size %d, replica %d, fsType %s, maxIops %d, maxBW %d, placementMode %s, template %s, cloneSrc %s",
		size, replica, fsType, maxIops, maxBW, placementMode, template, cloneSrc)
}

func doImplicitCreate(ctxt context.Context, d DateraDriver, name, m string) (*co.VolObj, error) {
	// We need to create this implicitly since DCOS doesn't support full
	// volume lifecycle management via Docker
	volOpts := dc.VolOpts{}
	setFromEnvs(ctxt, &volOpts)
	setDefaults(ctxt, &volOpts)

	d.Volumes[m] = co.UpsertVolObj(name, volOpts.FsType, 0, volOpts.Persistence)

	volObj, ok := d.Volumes[m]
	co.Debugf(ctxt, "volObj: %s, ok: %d", volObj, ok)

	co.Debugf(ctxt, "Sending DCOS IMPLICIT create-volume to datera server.")
	err := d.DateraClient.CreateVolume(ctxt, name, &volOpts)
	if err != nil {
		return nil, err
	}
	co.Debugf(ctxt, "Mounting implict volume: %s", name)
	vol, err := doMount(ctxt, d, name, volOpts.Persistence, volOpts.FsType)
	return vol, err
}

func doMount(ctxt context.Context, d DateraDriver, name, pmode, fs string) (*co.VolObj, error) {
	m := d.MountPoint(name)
	diskPath, err := d.DateraClient.LoginVolume(ctxt, name, m)
	if err != nil {
		co.Debugf(ctxt, "Couldn't find volume, error: %s", err)
		return nil, err
	}
	if diskPath == "" {
		return nil, fmt.Errorf("Disk path is not populated")
	}
	newfs, _ := d.DateraClient.FindDeviceFsType(ctxt, diskPath)
	newfs = strings.TrimSpace(newfs)
	fs = strings.TrimSpace(fs)
	// The device was previously created, but there is no filesystem
	// so we're going to use the default
	if newfs == "" && fs == "" {
		co.Debugf(ctxt, "Couldn't detect fs and parameter fs is not set for volume: %s", name)
		fs = DefaultFS
	} else if fs == "" && newfs != "" {
		co.Debugf(ctxt, "Setting volume: %s fs to detected type: %s", name, newfs)
		fs = newfs
	}
	vol := co.UpsertVolObj(name, fs, 0, pmode)
	if err = d.DateraClient.MountVolume(ctxt, name, m, fs, diskPath); err != nil {
		return vol, err
	}
	d.Volumes[m] = vol
	return vol, err
}

func isDcosDocker() bool {
	fwk := strings.ToLower(os.Getenv(EnvFwk))
	return fwk == "dcos-docker"
}

func isDcosMesos() bool {
	fwk := strings.ToLower(os.Getenv(EnvFwk))
	return fwk == "dcos-mesos"
}
