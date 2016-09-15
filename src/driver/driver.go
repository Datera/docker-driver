package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"datera-lib"
	"github.com/docker/go-plugins-helpers/volume"
)

const (
	DefaultFS       = "ext4"
	DefaultReplicas = 3
	DriverVersion   = datera.VERSION

	// MESOS Compatibility Environment Variables
	DATERA_VOLUME_NAME = "DATERA_VOLUME_NAME"
	DATERA_VOLUME_OPTS = "DATERA_VOLUME_OPTS"
	DRIVER             = "Docker-Volume"
	VERSION            = "1.0"
)

type volumeEntry struct {
	name        string
	fsType      string
	connections int
}

// Need to require interface instead of DateraClient directly
// so we can mock DateraClient out more easily
type ClientInterface interface {
	Login(string, string) error
	VolumeExist(string) (bool, error)
	CreateVolume(string, uint64, uint8, string, uint64, uint64) error
	StopVolume(string) error
	MountVolume(string, string, string) error
	UnmountVolume(string, string) error
	DetachVolume(string) error
	GetIQNandPortal(string) (string, string, string, error)
}

type DateraDriver struct {
	root         string
	DateraClient ClientInterface
	volumes      map[string]*volumeEntry
	m            *sync.Mutex
	version      string
	debug        bool
}

func NewDateraDriver(root, restAddress, dateraBase, username, password string, debug bool) DateraDriver {
	d := DateraDriver{
		root:    root,
		volumes: map[string]*volumeEntry{},
		m:       &sync.Mutex{},
		version: DriverVersion,
		debug:   debug,
	}
	log.Printf("DateraDriver: %#v", d)
	if len(restAddress) > 0 {
		log.Println(
			fmt.Sprintf("Creating DateraClient object with restAddress: [%#v]", restAddress))
		client := datera.NewClient(restAddress, dateraBase, username, password, debug, DRIVER, VERSION)
		d.DateraClient = client
	}
	log.Println(
		fmt.Sprintf("Driver Version: [%#v]", d.GetVersion()))
	return d
}

func (d DateraDriver) GetVolumeMap() map[string]*volumeEntry {
	return d.volumes
}

func (d DateraDriver) GetVersion() string {
	return d.version
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
func (d DateraDriver) Create(r volume.Request) volume.Response {
	log.Printf("DateraDriver.%#v", "Create")
	log.Printf("Creating volume %#v\n", r.Name)
	d.m.Lock()
	defer d.m.Unlock()
	m := d.mountpoint(r.Name)
	log.Printf("mountpoint for %#v is [%#v]", r.Name, m)
	volumeOptions := r.Options
	log.Printf("Volume Options: %#v", volumeOptions)
	if len(volumeOptions) == 0 {
		_, volopts, err := d.readEnv()
		if err != nil {
			return volume.Response{Err: err.Error()}
		}
		volumeOptions = volopts
	}
	size, _ := strconv.ParseUint(volumeOptions["size"], 10, 64)
	replica, _ := strconv.ParseUint(volumeOptions["replica"], 10, 8)
	template := volumeOptions["template"]
	fsType := volumeOptions["fsType"]
	maxIops, _ := strconv.ParseUint(volumeOptions["maxIops"], 10, 64)
	maxBW, _ := strconv.ParseUint(volumeOptions["maxBW"], 10, 64)

	// Set default filesystem to ext4
	if len(fsType) == 0 {
		log.Println(
			fmt.Sprintf("Using default filesystem value of %#v", DefaultReplicas))
		fsType = DefaultFS
	}

	// Set default replicas to 3
	if replica == 0 {
		log.Println(
			fmt.Sprintf("Using default replica value of %#v", DefaultReplicas))
		replica = DefaultReplicas
	}

	d.volumes[m] = &volumeEntry{name: r.Name, fsType: fsType, connections: 0}

	volEntry, ok := d.volumes[m]
	log.Printf("volEntry = [%#v], ok = [%d]", volEntry, ok)

	if d.DateraClient != nil {
		log.Printf("Checking for existing volume [%#v]", r.Name)
		exist, err := d.DateraClient.VolumeExist(r.Name)
		if err != nil {
			return volume.Response{Err: err.Error()}
		}

		if !exist {
			log.Printf("Sending create-volume to datera server.")
			if err := d.DateraClient.CreateVolume(
				r.Name,
				size,
				uint8(replica),
				template,
				maxIops,
				maxBW); err != nil {
				return volume.Response{Err: err.Error()}
			}
		}
	}
	return volume.Response{}
}

func (d DateraDriver) Remove(r volume.Request) volume.Response {
	log.Printf("DateraDriver.%#v", "Remove")
	log.Printf("Removing volume %#v\n", r.Name)
	d.m.Lock()
	defer d.m.Unlock()
	m := d.mountpoint(r.Name)

	log.Printf("Remove: mountpoint %#v", m)
	if s, ok := d.volumes[m]; ok {
		log.Printf("Remove: conection count ", s.connections)
		if s.connections <= 1 {
			if d.DateraClient != nil {
				if err := d.DateraClient.StopVolume(r.Name); err != nil {
					return volume.Response{Err: err.Error()}
				}
			}
			delete(d.volumes, m)
		}
	}
	return volume.Response{}
}

func (d DateraDriver) List(r volume.Request) volume.Response {
	log.Printf("DateraDriver.%#v", "List")
	log.Printf("Listing volumes: \n")
	d.m.Lock()
	defer d.m.Unlock()
	var vols []*volume.Volume
	for _, v := range d.volumes {
		if d.debug {
			log.Printf("Volume Name : [", v.name, "] mount-point [", d.mountpoint(v.name))
		}
		vols = append(vols, &volume.Volume{Name: v.name, Mountpoint: d.mountpoint(v.name)})
	}
	return volume.Response{Volumes: vols}
}

func (d DateraDriver) Get(r volume.Request) volume.Response {
	log.Printf("DateraDriver.%#v", "Get")
	log.Printf("Get volumes: %#v", r.Name)
	d.m.Lock()
	defer d.m.Unlock()
	m := d.mountpoint(r.Name)
	if s, ok := d.volumes[m]; ok {
		return volume.Response{Volume: &volume.Volume{Name: s.name, Mountpoint: d.mountpoint(s.name)}}
	}
	return volume.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", m)}
}

func (d DateraDriver) Path(r volume.Request) volume.Response {
	log.Printf("DateraDriver.%#v", "Path")
	return volume.Response{Mountpoint: d.mountpoint(r.Name)}
}

func (d DateraDriver) Mount(r volume.MountRequest) volume.Response {
	log.Printf("DateraDriver.%#v", "Mount")
	d.m.Lock()
	defer d.m.Unlock()
	m := d.mountpoint(r.Name)
	log.Printf("Mounting volume %#v on %#v\n", r.Name, m)

	s, ok := d.volumes[m]

	if !ok {
		return volume.Response{Err: fmt.Sprintf("Volume not found: %s", m)}
	}

	if ok && s.connections > 0 {
		s.connections++
		return volume.Response{Mountpoint: m}
	}

	fi, err := os.Lstat(m)

	if os.IsNotExist(err) {
		if err := os.MkdirAll(m, 0755); err != nil {
			return volume.Response{Err: err.Error()}
		}
	} else if err != nil {
		return volume.Response{Err: err.Error()}
	}

	if fi != nil && !fi.IsDir() {
		return volume.Response{Err: fmt.Sprintf("%v already exist and it's not a directory", m)}
	}

	if err := d.mountVolume(r.Name, m, s.fsType); err != nil {
		return volume.Response{Err: err.Error()}
	}

	d.volumes[m] = &volumeEntry{name: r.Name, fsType: s.fsType, connections: 1}

	return volume.Response{Mountpoint: m}
}

func (d DateraDriver) Unmount(r volume.UnmountRequest) volume.Response {
	log.Printf("DateraDriver.%#v", "Unmount")
	d.m.Lock()
	defer d.m.Unlock()
	m := d.mountpoint(r.Name)
	log.Printf("Driver::Unmount: unmounting volume %#v from %#v\n", r.Name, m)

	if s, ok := d.volumes[m]; ok {
		if s.connections == 1 {
			if err := d.unmountVolume(r.Name, m); err != nil {
				return volume.Response{Err: err.Error()}
			}
		}
		s.connections--
	} else {
		return volume.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", m)}
	}

	return volume.Response{}
}

func (d DateraDriver) Capabilities(r volume.Request) volume.Response {
	log.Printf("DateraDriver.%#v", "Capabilities")
	// TODO(mss): Add real backend capabilites to this shim
	return volume.Response{Capabilities: volume.Capability{Scope: "test"}}
}

func (d *DateraDriver) mountpoint(name string) string {
	return filepath.Join(d.root, name)
}

func (d *DateraDriver) mountVolume(name, destination, fsType string) error {
	err := d.DateraClient.MountVolume(name, destination, fsType)
	if err != nil {
		log.Println(
			fmt.Sprintf("Unable to mount the volume %#v at %#v", name, destination))
		return err
	}

	return nil
}

func (d *DateraDriver) unmountVolume(name, destination string) error {
	err := d.DateraClient.UnmountVolume(name, destination)
	if err != nil {
		log.Println(
			fmt.Sprintf("Unable to unmount the volume %#v at %#v", name, destination))
		return err
	}
	return nil
}

func (d *DateraDriver) readEnv() (string, map[string]string, error) {

	// Parse docker envs from this command
	cmd := `docker inspect --format "{{ index (index .Config.Env) }}" $(docker ps -a -l | tail -n1 | awk '{print $1}')`
	senvs, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	if err != nil {
		log.Println("Unable to determine the most recent docker container")
		return "", make(map[string]string), err
	}

	envs := strings.Split(strings.Trim(string(senvs), "[]"), " ")
	log.Println(
		fmt.Sprintf("Docker Env Vars: %s", envs))

	envmap := stringArrayToMap(envs, "=")

	volname := envmap[DATERA_VOLUME_NAME]
	sopts := envmap[DATERA_VOLUME_OPTS]

	opts := strings.Split(sopts, ",")
	log.Println(
		fmt.Sprintf("Found environment var: [%#v]=[%#v]", DATERA_VOLUME_NAME, volname))
	log.Println(
		fmt.Sprintf("Found environment var: [%#v]=[%#v]", DATERA_VOLUME_OPTS, sopts))

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
