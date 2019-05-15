package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	dv "github.com/docker/go-plugins-helpers/volume"

	co "github.com/Datera/docker-driver/pkg/common"
	udc "github.com/Datera/go-udc/pkg/udc"

	dc "github.com/Datera/datera-csi/pkg/client"
)

const (
	DefaultSize        = 16
	DefaultFS          = "ext4"
	DefaultReplicas    = 3
	DefaultPlacement   = "hybrid"
	DefaultPersistence = "manual"
	DriverVersion      = "unset"
	SdkVersion         = "unset"
	Githash            = "unset"
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
	// 2018.5.1.0 -- Switched to new date-based versioning scheme
	// 2019.5.10.0 -- Major rework, update to modern go-sdk, moving to csi-lib-iscsi
	//                moving to csi client instead of custom client

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

var (
	Opts = map[string][]string{
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
	topctxt = context.WithValue(context.Background(), "host", host)
	host, _ = os.Hostname()
)

type DateraDriver struct {
	DateraClient *dc.DateraClient
	Mutex        *sync.Mutex
	Version      string
	Debug        bool
	Ssl          bool
}

func NewDateraDriver(conf *udc.UDC) DateraDriver {
	d := DateraDriver{
		Mutex:   &sync.Mutex{},
		Version: DriverVersion,
		Debug:   true,
	}
	v := fmt.Sprintf("docker-driver-%s-%s-gosdk-%s", DriverVersion, Githash, SdkVersion)
	client, err := dc.NewDateraClient(conf, true, v)
	if err != nil {
		panic(err)
	}
	d.DateraClient = client
	ctxt := d.initFunc("NewDateraDriver")
	co.Debugf(ctxt, "Creating DateraClient object with restAddress: %s", conf.MgmtIp)
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
func (d *DateraDriver) Create(r *dv.CreateRequest) error {
	ctxt := d.initFunc("Create")
	co.Debugf(ctxt, "DateraDriver.Create: %#v", r)
	co.Debugf(ctxt, "Creating volume %s\n", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	co.Debugf(ctxt, "Mountpoint for Request %s is %s", r.Name, m)
	volOpts := r.Options
	co.Debugf(ctxt, "Volume Options: %#v", volOpts)

	co.Debugf(ctxt, "Checking for existing volume: %s", r.Name)
	_, err := d.DateraClient.GetVolume(r.Name, true, true)
	if err == nil {
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
	// persistence, _ := volOpts[OptPersistence]
	cloneSrc, _ := volOpts[OptCloneSrc]

	vOpts := dc.VolOpts{
		Size:              int(size),
		Replica:           int(replica),
		Template:          template,
		FsType:            fsType,
		PlacementMode:     placementMode,
		CloneSrc:          cloneSrc,
		TotalIopsMax:      int(maxIops),
		TotalBandwidthMax: int(maxBW),
		IpPool:            "default",
	}

	co.Debugf(ctxt, "Passed in volume opts: %s", co.Prettify(vOpts))

	vol, err := d.DateraClient.CreateVolume(r.Name, &vOpts, true)
	if err != nil {
		return err
	}

	// Set metadata values for Persistence and FsType so Mount can find them later
	if _, err = vol.SetMetadata(&dc.VolMetadata{OptPersistence: DefaultPersistence, OptFstype: vOpts.FsType}); err != nil {
		return err
	}
	return nil
}

func (d *DateraDriver) Remove(r *dv.RemoveRequest) error {
	ctxt := d.initFunc("Remove")
	co.Debugf(ctxt, "DateraDriver.Remove: %#v", r)
	co.Debugf(ctxt, "Removing volume %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)

	co.Debugf(ctxt, "Remove: mountpoint %s", m)
	vol, err := d.DateraClient.GetVolume(r.Name, false, false)
	if err != nil {
		co.Debugf(ctxt, "Could not find volume with name %s", r.Name)
	}
	vol.MountPath = m
	if err := vol.Unmount(); err != nil {
		co.Warningf(ctxt, "Error unmounting volume: %s", err)
	}
	if err := vol.Delete(true); err != nil {
		// Don't return an error if we fail to delete the volume
		// this provides a better user experience.  Log the error
		// so it can be debugged if needed
		co.Warningf(ctxt, "Error deleting volume: %s", err)
		return nil
	}
	return nil
}

func (d *DateraDriver) List() (*dv.ListResponse, error) {
	ctxt := d.initFunc("List")
	co.Debugf(ctxt, "DateraDriver.List")
	co.Debugf(ctxt, "Listing volumes")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	var vols []*dv.Volume
	dvols, err := d.DateraClient.ListVolumes(0, 0)
	if err != nil {
		return &dv.ListResponse{}, err
	}
	for _, v := range dvols {
		co.Debugf(ctxt, "Volume Name: %s mount-point: %s", v.Name, d.MountPoint(v.Name))
		vols = append(vols, &dv.Volume{Name: v.Name, Mountpoint: d.MountPoint(v.Name)})
	}
	return &dv.ListResponse{Volumes: vols}, nil
}

func (d *DateraDriver) Get(r *dv.GetRequest) (*dv.GetResponse, error) {
	ctxt := d.initFunc("Get")
	co.Debugf(ctxt, "DateraDriver.Get: %#v", r)
	co.Debugf(ctxt, "Get volume: %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	if _, err := d.DateraClient.GetVolume(r.Name, true, true); err == nil {
		return &dv.GetResponse{Volume: &dv.Volume{Name: r.Name, Mountpoint: d.MountPoint(r.Name), Status: make(map[string]interface{}, 0)}}, nil
	} else {
		return &dv.GetResponse{}, nil
	}
}

func (d *DateraDriver) Path(r *dv.PathRequest) (*dv.PathResponse, error) {
	ctxt := d.initFunc("Path")
	co.Debugf(ctxt, "DateraDriver.Path")
	return &dv.PathResponse{Mountpoint: d.MountPoint(r.Name)}, nil
}

func (d *DateraDriver) Mount(r *dv.MountRequest) (*dv.MountResponse, error) {
	ctxt := d.initFunc("Mount")
	co.Debugf(ctxt, "DateraDriver.Mount: %#v", r)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	co.Debugf(ctxt, "Mounting volume %s on %s\n", r.Name, m)

	if _, err := d.DateraClient.GetVolume(r.Name, false, false); err != nil {
		err := fmt.Errorf("Volume not found: %s", m)
		co.Errorf(ctxt, "Failed Mount: %s", err)
		return &dv.MountResponse{}, err
	}

	err := doMount(ctxt, d, r.Name, DefaultPersistence, DefaultFS)
	if err != nil {
		return &dv.MountResponse{}, err
	}
	return &dv.MountResponse{Mountpoint: m}, nil
}

func (d *DateraDriver) Unmount(r *dv.UnmountRequest) error {
	ctxt := d.initFunc("Unmount")
	co.Debugf(ctxt, "DateraDriver.Unmount: %#v", r)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.MountPoint(r.Name)
	co.Debugf(ctxt, "Driver::Unmount: unmounting volume %s from %s\n", r.Name, m)

	vol, err := d.DateraClient.GetVolume(r.Name, false, false)
	if err != nil {
		co.Debugf(ctxt, "Could not find volume with name %s", r.Name)
	}
	vol.MountPath = m
	if err := vol.Unmount(); err != nil {
		co.Errorf(ctxt, "Unmount Error: %s", err)
	}
	init, err := d.DateraClient.CreateGetInitiator()
	if err != nil {
		co.Warning(ctxt, err)
	}
	err = vol.UnregisterAcl(init)
	if err != nil {
		co.Warning(ctxt, err)
	}
	return nil
}

func (d *DateraDriver) Capabilities() *dv.CapabilitiesResponse {
	ctxt := d.initFunc("Capabilities")
	co.Debugf(ctxt, "DateraDriver.Capabilities")
	// This driver is global scope since created volumes are not bound to the
	// engine that created them.
	return &dv.CapabilitiesResponse{Capabilities: dv.Capability{Scope: "global"}}
}

func (d *DateraDriver) MountPoint(name string) string {
	return filepath.Join(MountLoc, name)
}

func (d *DateraDriver) initFunc(reqName string) context.Context {
	ctxt := context.WithValue(topctxt, co.TraceId, co.GenId())
	ctxt = context.WithValue(ctxt, co.ReqName, reqName)
	ctxt = d.DateraClient.WithContext(ctxt)
	return ctxt
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
	co.Debugf(ctxt, "After setting defaults: size %d, fsType %s, replica %d, placementMode %s",
		volOpts.Size, volOpts.FsType, volOpts.Replica, volOpts.PlacementMode)
}

func doMount(ctxt context.Context, d *DateraDriver, name, pmode, fs string) error {
	m := d.MountPoint(name)
	vol, err := d.DateraClient.GetVolume(name, true, true)
	if err != nil {
		co.Debugf(ctxt, "Couldn't find volume with name: %s", name)
		return nil
	}
	init, err := d.DateraClient.CreateGetInitiator()
	if err != nil {
		return err
	}
	if err = vol.RegisterAcl(init); err != nil {
		return err
	}
	// TODO: Fix multipath support post-refactor
	if err := vol.Login(false, false); err != nil {
		co.Errorf(ctxt, "Couldn't login volume, error: %s", err)
		return err
	}
	diskPath := vol.DevicePath
	if diskPath == "" {
		err = fmt.Errorf("Disk path is not populated")
		co.Error(ctxt, err)
		return err
	}
	if err = vol.Format(fs, []string{}, 180); err != nil {
		return err
	}
	if err = vol.Mount(m, []string{}, fs); err != nil {
		return err
	}
	return nil
}
