package driver

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	dc "ddd/client"
	co "ddd/common"
	dsdk "github.com/Datera/go-sdk/src/dsdk"
	dv "github.com/docker/go-plugins-helpers/volume"
)

const (
	DefaultSize        = 16
	DefaultFS          = "ext4"
	DefaultReplicas    = 3
	DefaultPlacement   = "hybrid"
	DefaultPersistence = "manual"
	DriverVersion      = "2.0.3"
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
	// 2.0.0 -- Moved Environment variables to config files, removed DB layer
	//          FSType and persistence are tracked via metadata now.  Updated
	//			Makefile to ensure static binaries are built. Added several
	//			helper functions in util.go
	// 2.0.1 -- Added /etc/iscsi as a rbind, rshared mount in the plugin config.json
	// 2.0.2 -- Removed deprecated environment variables, updated to go-sdk 1.0.7
	// 2.0.3 -- Removed unused tests, added -print-opts cli option, cleaned up
	//          some clutter in util.go and added "make fast" to Makefile

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
	MountLoc = "/mnt"

	// Misc
	DeleteConst = "auto"
)

var Opts = map[string][]string{
	OptSize:        []string{"Volume Size In GiB", strconv.Itoa(DefaultSize)},
	OptReplica:     []string{"Volume Replicas", strconv.Itoa(DefaultReplicas)},
	OptTemplate:    []string{"Volume Template", "None"},
	OptFstype:      []string{"Volume Filesystem", DefaultFS},
	OptMaxiops:     []string{"Volume Max Total IOPS", "0"},
	OptMaxbw:       []string{"Volume Max Total Bandwidth", "0"},
	OptPlacement:   []string{"Volume Placement", DefaultPlacement},
	OptPersistence: []string{"Volume Persistence", DefaultPersistence},
	OptCloneSrc:    []string{"Volume Source For Clone", "None"},
}

// Need to require interface instead of DateraClient directly
// so we can mock DateraClient out more easily
type IClient interface {
	GetVolume(context.Context, string) (*dsdk.AppInstance, error)
	CreateVolume(context.Context, string, *dc.VolOpts) error
	DeleteVolume(context.Context, string, string) error
	LoginVolume(context.Context, string, string) (string, error)
	MountVolume(context.Context, string, string, string, string) error
	UnmountVolume(context.Context, string, string) error
	DetachVolume(context.Context, string) error
	GetIQNandPortals(context.Context, string) (string, []string, string, error)
	FindDeviceFsType(context.Context, string) (string, error)
	ListVolumes(context.Context) ([]string, error)
	GetMetadata(context.Context, string) (*map[string]interface{}, error)
	PutMetadata(context.Context, string, *map[string]interface{}) error
}

type DateraDriver struct {
	DateraClient IClient
	Mutex        *sync.Mutex
	Version      string
	Debug        *bool
	Ssl          bool
	Conf         *dc.Config
}

func NewDateraDriver(conf *dc.Config) DateraDriver {
	d := DateraDriver{
		Mutex:   &sync.Mutex{},
		Version: DriverVersion,
		Debug:   &conf.Debug,
		Conf:    conf,
	}
	ctxt := co.MkCtxt("NewDateraDriver")
	co.Debugf(ctxt, "Creating DateraClient object with restAddress: %s", conf.DateraCluster)
	client := dc.NewClient(ctxt, conf)
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
	ai, err := d.DateraClient.GetVolume(ctxt, r.Name)
	if err == nil && ai != nil {
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
	if isDcosDocker(d.Conf) {
		d.setFromConf(ctxt, &vOpts)
	}

	setDefaults(ctxt, &vOpts)

	err = d.DateraClient.CreateVolume(ctxt, r.Name, &vOpts)
	if err != nil {
		return err
	}

	// Set metadata values for Persistence and FsType so Mount can find them later
	err = d.DateraClient.PutMetadata(ctxt, r.Name, &map[string]interface{}{OptPersistence: vOpts.Persistence, OptFstype: vOpts.FsType})
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
	if err := d.DateraClient.UnmountVolume(ctxt, r.Name, m); err != nil {
		co.Warningf(ctxt, "Error unmounting volume: %s", err)
	}
	if err := d.DateraClient.DeleteVolume(ctxt, r.Name, m); err != nil {
		// Don't return an error if we fail to delete the volume
		// this provides a better user experience.  Log the error
		// so it can be debugged if needed
		co.Warningf(ctxt, "Error deleting volume: %s", err)
		return nil
	}
	return nil
}

func (d DateraDriver) List() (*dv.ListResponse, error) {
	ctxt := co.MkCtxt("List")
	co.Debugf(ctxt, "DateraDriver.List")
	co.Debugf(ctxt, "Listing volumes")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	var vols []*dv.Volume
	vNames, err := d.DateraClient.ListVolumes(ctxt)
	if err != nil {
		return &dv.ListResponse{}, err
	}
	for _, v := range vNames {
		co.Debugf(ctxt, "Volume Name: %s mount-point: %s", v, d.MountPoint(v))
		vols = append(vols, &dv.Volume{Name: v, Mountpoint: d.MountPoint(v)})
	}
	return &dv.ListResponse{Volumes: vols}, nil
}

func (d DateraDriver) Get(r *dv.GetRequest) (*dv.GetResponse, error) {
	ctxt := co.MkCtxt("Get")
	co.Debugf(ctxt, "DateraDriver.Get: %#v", r)
	co.Debugf(ctxt, "Get volume: %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	if ai, err := d.DateraClient.GetVolume(ctxt, r.Name); ai != nil && err == nil {
		return &dv.GetResponse{Volume: &dv.Volume{Name: r.Name, Mountpoint: d.MountPoint(r.Name), Status: make(map[string]interface{}, 0)}}, nil
	} else if isDcosDocker(d.Conf) {
		if err = doImplicitCreate(ctxt, d, r.Name, m); err != nil {
			return &dv.GetResponse{}, err
		}
		return &dv.GetResponse{Volume: &dv.Volume{Name: r.Name, Mountpoint: d.MountPoint(r.Name), Status: make(map[string]interface{}, 0)}}, nil
	} else {
		return &dv.GetResponse{}, nil
	}
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

	// vol, ok := d.Volumes[m]

	if ai, err := d.DateraClient.GetVolume(ctxt, r.Name); ai == nil || err != nil {
		err := fmt.Errorf("Volume not found: %s", m)
		co.Errorf(ctxt, "Failed Mount: %s", err)
		return &dv.MountResponse{}, err
	}

	p, f, err := OptsFromMeta(ctxt, d, r.Name)
	if err != nil {
		return &dv.MountResponse{}, err
	}

	err = doMount(ctxt, d, r.Name, p, f)
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

	if err := d.DateraClient.UnmountVolume(ctxt, r.Name, m); err != nil {
		co.Errorf(ctxt, "Unmount Error: %s", err)
	}

	p, _, err := OptsFromMeta(ctxt, d, r.Name)
	if err != nil {
		return err
	}

	if p == DeleteConst {
		co.Debugf(ctxt, "Volume %s persistence mode is set to: %s, deleting volume after unmount", r.Name, p)
		if err := d.DateraClient.DeleteVolume(ctxt, r.Name, m); err != nil {
			co.Errorf(ctxt, "Deletion Error: %s", err)
		}
	} else {
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

func (d *DateraDriver) setFromConf(ctxt context.Context, volOpts *dc.VolOpts) {
	v := d.Conf.Volume
	volOpts.Size = v.Size
	volOpts.Replica = v.Replica
	volOpts.Template = v.Template
	volOpts.FsType = v.FsType
	volOpts.MaxIops = v.MaxIops
	volOpts.MaxBW = v.MaxBW
	volOpts.PlacementMode = v.PlacementMode
	volOpts.Persistence = v.Persistence
	volOpts.CloneSrc = v.CloneSrc
	co.Debugf(ctxt, "Reading values from Config: %#v", volOpts)
}

func doImplicitCreate(ctxt context.Context, d DateraDriver, name, m string) error {
	// We need to create this implicitly since DCOS doesn't support full
	// volume lifecycle management via Docker
	volOpts := dc.VolOpts{}
	d.setFromConf(ctxt, &volOpts)
	setDefaults(ctxt, &volOpts)

	// d.Volumes[m] = co.UpsertVolObj(name, volOpts.FsType, 0, volOpts.Persistence)

	// volObj, ok := d.Volumes[m]
	// co.Debugf(ctxt, "volObj: %s, ok: %d", volObj, ok)

	co.Debugf(ctxt, "Sending DCOS IMPLICIT create-volume to datera server.")
	err := d.DateraClient.CreateVolume(ctxt, name, &volOpts)

	// Set metadata values for Persistence and FsType so Mount can find them later
	err = d.DateraClient.PutMetadata(ctxt, name, &map[string]interface{}{OptPersistence: volOpts.Persistence, OptFstype: volOpts.FsType})
	if err != nil {
		return err
	}
	co.Debugf(ctxt, "Mounting implict volume: %s", name)
	err = doMount(ctxt, d, name, volOpts.Persistence, volOpts.FsType)
	return err
}

func doMount(ctxt context.Context, d DateraDriver, name, pmode, fs string) error {
	m := d.MountPoint(name)
	diskPath, err := d.DateraClient.LoginVolume(ctxt, name, m)
	if err != nil {
		co.Errorf(ctxt, "Couldn't find volume, error: %s", err)
		return err
	}
	if diskPath == "" {
		err = fmt.Errorf("Disk path is not populated")
		co.Error(ctxt, err)
		return err
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
	// vol := co.UpsertVolObj(name, fs, 0, pmode)
	if err = d.DateraClient.MountVolume(ctxt, name, m, fs, diskPath); err != nil {
		return err
	}
	// d.Volumes[m] = vol
	return nil
}

func OptsFromMeta(ctxt context.Context, d DateraDriver, name string) (string, string, error) {
	meta, err := d.DateraClient.GetMetadata(ctxt, name)
	if err != nil {
		co.Errorf(ctxt, "Failed to get metadata for vol %s. err: %s", name, err)
		return "", "", err
	} else if meta == nil {
		co.Errorf(ctxt, "No metadata found for vol %s.", name)
	}
	k := (*meta)[OptPersistence]
	var p string
	if k == nil {
		p = "manual"
	} else {
		p = k.(string)
	}
	k = (*meta)[OptFstype]
	var f string
	if k == nil {
		f = "ext4"
	} else {
		f = k.(string)
	}
	return p, f, nil
}

func isDcosDocker(conf *dc.Config) bool {
	fwk := strings.ToLower(conf.Framework)
	return fwk == "dcos-docker"
}

func isDcosMesos(conf *dc.Config) bool {
	fwk := strings.ToLower(conf.Framework)
	return fwk == "dcos-mesos"
}
