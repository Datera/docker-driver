package main

import (
	"fmt"
	"os"
	"os/exec"
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
	DriverVersion   = "1.0.2"
	DRIVER          = "Docker-Volume"

	// MESOS Compatibility Environment Variables
	DATERA_VOLUME_NAME = "DATERA_VOLUME_NAME"
	DATERA_VOLUME_OPTS = "DATERA_VOLUME_OPTS"
)

type VolumeEntry struct {
	name        string
	fsType      string
	connections int
}

// Need to require interface instead of DateraClient directly
// so we can mock DateraClient out more easily
type ClientInterface interface {
	VolumeExist(string) (bool, error)
	CreateVolume(string, int, int, string, int, int) error
	DeleteVolume(string) error
	MountVolume(string, string, string) error
	UnmountVolume(string, string) error
	DetachVolume(string) error
	GetIQNandPortal(string) (string, string, string, error)
}

type DateraDriver struct {
	Root         string
	DateraClient ClientInterface
	Volumes      map[string]*VolumeEntry
	Mutex        *sync.Mutex
	Version      string
	Debug        bool
	Ssl          bool
}

func NewDateraDriver(root, restAddress, dateraBase, username, password, tenant string, debug, noSsl bool) DateraDriver {
	d := DateraDriver{
		Root:    root,
		Volumes: map[string]*VolumeEntry{},
		Mutex:   &sync.Mutex{},
		Version: DriverVersion,
		Debug:   debug,
	}
	log.Debugf(
		"Creating DateraClient object with restAddress: %s", restAddress)
	client := NewClient(restAddress, dateraBase, username, password, tenant, debug, !noSsl, DRIVER, DriverVersion)
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
func (d DateraDriver) Create(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%s", "Create")
	log.Debugf("Creating volume %s\n", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.mountPoint(r.Name)
	log.Debugf("Mountpoint for Request %s is %s", r.Name, m)
	volOpts := r.Options
	log.Debugf("Volume Options: %#v", volOpts)

	log.Debugf("Checking for existing volume: %s", r.Name)
	exist, err := d.DateraClient.VolumeExist(r.Name)
	if err != nil {
		return dv.Response{Err: err.Error()}
	}
	if exist {
		log.Debugf("Found already created volume: ", r.Name)
		return dv.Response{}
	}
	// Handle Mesosphere case where we read environment variables
	if len(volOpts) == 0 && !exist {
		_, envopts, err := d.readEnv()
		if err != nil {
			return dv.Response{Err: err.Error()}
		}
		volOpts = envopts
	}

	size, _ := strconv.ParseUint(volOpts["size"], 10, 64)
	replica, _ := strconv.ParseUint(volOpts["replica"], 10, 8)
	template := volOpts["template"]
	fsType := volOpts["fsType"]
	maxIops, _ := strconv.ParseUint(volOpts["maxIops"], 10, 64)
	maxBW, _ := strconv.ParseUint(volOpts["maxBW"], 10, 64)

	// Set default filesystem to ext4
	if len(fsType) == 0 {
		log.Debugf(
			"Using default filesystem value of %s", DefaultReplicas)
		fsType = DefaultFS
	}

	// Set default replicas to 3
	if replica == 0 {
		log.Debugf("Using default replica value of %d", DefaultReplicas)
		replica = DefaultReplicas
	}

	d.Volumes[m] = &VolumeEntry{name: r.Name, fsType: fsType, connections: 0}

	volEntry, ok := d.Volumes[m]
	log.Debugf("volEntry: %s, ok: %d", volEntry, ok)

	log.Debugf("Sending create-volume to datera server.")
	if err := d.DateraClient.CreateVolume(
		r.Name,
		int(size),
		int(replica),
		template,
		int(maxIops),
		int(maxBW)); err != nil {
		return dv.Response{Err: err.Error()}
	}
	return dv.Response{}
}

func (d DateraDriver) Remove(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%#v", "Remove")
	log.Debugf("Removing volume %#v\n", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.mountPoint(r.Name)

	log.Debugf("Remove: mountpoint %#v", m)
	if s, ok := d.Volumes[m]; ok {
		log.Debugf("Remove: conection count ", s.connections)
		if s.connections <= 1 {
			if d.DateraClient != nil {
				if err := d.DateraClient.DeleteVolume(r.Name); err != nil {
					return dv.Response{Err: err.Error()}
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
		log.Debugf("Volume Name: %s mount-point:  %s", v.name, d.mountPoint(v.name))
		vols = append(vols, &dv.Volume{Name: v.name, Mountpoint: d.mountPoint(v.name)})
	}
	return dv.Response{Volumes: vols}
}

func (d DateraDriver) Get(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%s", "Get")
	log.Debugf("Get volumes: %s", r.Name)
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.mountPoint(r.Name)
	if s, ok := d.Volumes[m]; ok {
		return dv.Response{Volume: &dv.Volume{Name: s.name, Mountpoint: d.mountPoint(s.name)}}
	}
	return dv.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", m)}
}

func (d DateraDriver) Path(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%s", "Path")
	return dv.Response{Mountpoint: d.mountPoint(r.Name)}
}

func (d DateraDriver) Mount(r dv.MountRequest) dv.Response {
	log.Debugf("DateraDriver.%s", "Mount")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.mountPoint(r.Name)
	log.Debugf("Mounting volume %s on %#v\n", r.Name, m)

	s, ok := d.Volumes[m]

	if !ok {
		return dv.Response{Err: fmt.Sprintf("Volume not found: %s", m)}
	}

	if ok && s.connections > 0 {
		s.connections++
		return dv.Response{Mountpoint: m}
	}

	fi, err := os.Lstat(m)

	if os.IsNotExist(err) {
		if err := os.MkdirAll(m, 0755); err != nil {
			return dv.Response{Err: err.Error()}
		}
	} else if err != nil {
		return dv.Response{Err: err.Error()}
	}

	if fi != nil && !fi.IsDir() {
		return dv.Response{Err: fmt.Sprintf("%v already exist and it's not a directory", m)}
	}

	if err := d.mountVolume(r.Name, m, s.fsType); err != nil {
		return dv.Response{Err: err.Error()}
	}

	d.Volumes[m] = &VolumeEntry{name: r.Name, fsType: s.fsType, connections: 1}

	return dv.Response{Mountpoint: m}
}

func (d DateraDriver) Unmount(r dv.UnmountRequest) dv.Response {
	log.Debugf("DateraDriver.%#v", "Unmount")
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	m := d.mountPoint(r.Name)
	log.Debugf("Driver::Unmount: unmounting volume %#v from %#v\n", r.Name, m)

	if s, ok := d.Volumes[m]; ok {
		if s.connections == 1 {
			if err := d.unmountVolume(r.Name, m); err != nil {
				return dv.Response{Err: err.Error()}
			}
		}
		s.connections--
	} else {
		return dv.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", m)}
	}

	return dv.Response{}
}

func (d DateraDriver) Capabilities(r dv.Request) dv.Response {
	log.Debugf("DateraDriver.%#v", "Capabilities")
	// TODO(mss): Add real backend capabilites to this shim
	return dv.Response{Capabilities: dv.Capability{Scope: "test"}}
}

func (d *DateraDriver) mountPoint(name string) string {
	return filepath.Join(d.Root, name)
}

func (d *DateraDriver) mountVolume(name, destination, fsType string) error {
	err := d.DateraClient.MountVolume(name, destination, fsType)
	if err != nil {
		log.Debugf(
			"Unable to mount the volume: %s at: %s", name, destination)
		return err
	}

	return nil
}

func (d *DateraDriver) unmountVolume(name, destination string) error {
	err := d.DateraClient.UnmountVolume(name, destination)
	if err != nil {
		log.Debugf(
			"Unable to unmount the volume %#v at %#v", name, destination)
		return err
	}
	return nil
}

func (d *DateraDriver) readEnv() (string, map[string]string, error) {

	// Parse docker envs from this command
	cmd := `docker inspect --format "{{ index (index .Config.Env) }}" $(docker ps -a -l | tail -n1 | awk '{print $1}')`
	senvs, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	if err != nil {
		log.Debugf("Unable to determine the most recent docker container")
		return "", make(map[string]string), err
	}

	envs := strings.Split(strings.Trim(string(senvs), "[]"), " ")
	log.Debugf(
		"Docker Env Vars: %s", envs)

	envmap := stringArrayToMap(envs, "=")

	volname := envmap[DATERA_VOLUME_NAME]
	sopts := envmap[DATERA_VOLUME_OPTS]

	opts := strings.Split(sopts, ",")
	log.Debugf(
		"Found environment var: %s=%s", DATERA_VOLUME_NAME, volname)
	log.Debugf(
		"Found environment var: %s=%s", DATERA_VOLUME_OPTS, sopts)

	optsresult := stringArrayToMap(opts, "=")
	// These are comma separated.  If the first RequiredKeys is not present
	// the second set will be checked before raising an error.
	RequiredKeys1 := "size"
	RequiredKeys2 := "template"

	// Check for first required key
	for _, k := range strings.Split(RequiredKeys1, ",") {
		if _, ok := optsresult[k]; !ok {
			// If the first key isn't present, check for the second one
			for _, k2 := range strings.Split(RequiredKeys2, ",") {
				if _, ok2 := optsresult[k2]; !ok2 {
					// Raise an error if neither Key set is found
					err := fmt.Errorf("Required key: [%#v or %#v] not found in environment variable [%#v]",
						k,
						k2,
						DATERA_VOLUME_OPTS)
					return volname, optsresult, err
				}
			}
		}
	}

	return volname, optsresult, nil
}

func stringArrayToMap(array []string, sep string) map[string]string {
	result := make(map[string]string)
	for _, item := range array {
		// Only split into two substrings, otherwise we'll run into issues
		// When values have the same separator as the key/value
		s := strings.SplitN(item, sep, 2)
		result[s[0]] = s[1]
	}
	return result
}
