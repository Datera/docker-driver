package client

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	co "ddd/common"
	dsdk "github.com/Datera/go-sdk/src/dsdk"
)

const (
	initiatorFile = "/etc/iscsi/initiatorname.iscsi"
	StorageName   = "storage-1"
	VolumeName    = "volume-1"
	Prefix        = "DVOLD-"
)

type Client struct {
	Debug *bool
	Api   *dsdk.SDK
	Conf  *Config
}

type Config struct {
	DateraCluster string   `json:"datera-cluster"`
	Username      string   `json:"username"`
	Password      string   `json:"password"`
	Ssl           bool     `json:"ssl"`
	Tenant        string   `json:"tenant,omitempty"`
	OsUser        string   `json:"os-user,omitempty"`
	Debug         bool     `json:"debug,omitempty"`
	Framework     string   `json:"framework,omitempty"`
	Volume        *VolOpts `json:"volume,omitempty"`
}

type VolOpts struct {
	Size          uint64 `json:"size,omitempty"`
	Replica       uint64 `json:"replica,omitempty"`
	Template      string `json:"template,omitempty"`
	FsType        string `json:"fstype,omitempty"`
	MaxIops       uint64 `json:"maxiops,omitempty"`
	MaxBW         uint64 `json:"maxbw,omitempty"`
	PlacementMode string `json:"placement,omitempty"`
	Persistence   string `json:"persistence,omitempty"`
	CloneSrc      string `json:"clone-src,omitempty"`
}

func NewClient(ctxt context.Context, conf *Config) *Client {
	headers := make(map[string]string)
	Api, err := dsdk.NewSDK(conf.DateraCluster, conf.Username, conf.Password, "2.1", conf.Tenant, "30s", headers, false, "", true)
	co.PanicErr(err)
	client := &Client{
		Api:   Api,
		Debug: &conf.Debug,
	}
	co.Debugf(ctxt, "Client: %#v", client)
	return client
}

func (r Client) GetMetadata(ctxt context.Context, name string) (*map[string]interface{}, error) {
	co.Debugf(ctxt, "GetMetadata invoked for %s", name)
	meta, err := r.Api.GetEp("app_instances").GetEp(getName(name)).GetEp("metadata").Get(ctxt)
	if err != nil {
		co.Debugf(ctxt, "Metadata for volume %s not found. err: %s", name, err)
		return nil, err
	}
	m := make(map[string]interface{}, 10)
	co.Debugf(ctxt, "Metadata raw: %s", string(meta.GetB()))
	if err = co.Unpack(meta.GetB(), &m); err != nil {
		return nil, err
	}
	co.Debugf(ctxt, "Pulled Metadata from volume %s.  Metadata: %s", name, co.Prettify(m))
	return &m, nil
}

func (r Client) PutMetadata(ctxt context.Context, name string, m *map[string]interface{}) error {
	co.Debugf(ctxt, "PutMetadata invoked for %s", name)
	co.Debugf(ctxt, "Setting metadata for volume %s, metadata %s", name, co.Prettify(m))
	_, err := r.Api.GetEp("app_instances").GetEp(getName(name)).GetEp("metadata").Set(ctxt, *m)
	if err != nil {
		return err
	}
	return nil
}

func (r Client) GetVolume(ctxt context.Context, name string) (*dsdk.AppInstance, error) {
	co.Debugf(ctxt, "GetVolume invoked for %s", name)
	name = getName(name)
	ai, err := r.Api.GetEp("app_instances").GetEp(name).Get(ctxt)
	if err != nil {
		co.Debugf(ctxt, "Volume %s not found. err: %s", name, err)
		return nil, err
	}
	co.Debugf(ctxt, "Volume %s found", name)
	myAi, err := dsdk.NewAppInstance(ai.GetB())
	if err != nil {
		co.Errorf(ctxt, "Could not unpack volume %s, err %s", name, err)
		return nil, err
	}
	return &myAi, nil
}

func (r Client) CreateVolume(ctxt context.Context, name string, volOpts *VolOpts) error {
	co.Debugf(ctxt, "CreateVolume invoked for %s, volOpts: %s", name, co.Prettify(volOpts))
	name = getName(name)
	var ai dsdk.AppInstance
	if volOpts.Template != "" {
		template := strings.Trim(volOpts.Template, "/")
		co.Debugf(ctxt, "Creating AppInstance with template: %s", template)
		at := dsdk.AppTemplate{
			Path: "/app_templates/" + template,
		}
		ai = dsdk.AppInstance{
			Name:        name,
			AppTemplate: &at,
		}
	} else if volOpts.CloneSrc != "" {
		c := map[string]string{"path": "/app_instances/" + volOpts.CloneSrc}
		co.Debugf(ctxt, "Creating AppInstance from clone: %s", volOpts.CloneSrc)
		ai = dsdk.AppInstance{
			Name:     name,
			CloneSrc: c,
		}
	} else {
		vol := dsdk.Volume{
			Name:          VolumeName,
			Size:          float64(volOpts.Size),
			PlacementMode: volOpts.PlacementMode,
			ReplicaCount:  int(volOpts.Replica),
		}
		si := dsdk.StorageInstance{
			Name:    StorageName,
			Volumes: &[]dsdk.Volume{vol},
		}
		ai = dsdk.AppInstance{
			Name:             name,
			StorageInstances: &[]dsdk.StorageInstance{si},
		}
	}
	_, err := r.Api.GetEp("app_instances").Create(ctxt, ai)
	if err != nil {
		co.Error(ctxt, err)
		return err
	}
	// Handle QoS values
	if volOpts.MaxIops != 0 || volOpts.MaxBW != 0 {
		pp := dsdk.PerformancePolicy{
			TotalIopsMax:      int(volOpts.MaxIops),
			TotalBandwidthMax: int(volOpts.MaxBW),
		}
		// Get Performance_policy endpoint
		_, err = r.Api.GetEp("app_instances").GetEp(name).GetEp(
			"storage_instances").GetEp(StorageName).GetEp(
			"volumes").GetEp(VolumeName).GetEp(
			"performance_policy").Create(ctxt, pp)
	}

	return nil
}

func (r Client) CreateACL(ctxt context.Context, name string, random bool) error {
	co.Debugf(ctxt, "CreateACL invoked for %s", name)
	name = getName(name)
	initiator, err := getInitName(ctxt)
	if err != nil {
		co.Error(ctxt, err)
		return err
	}

	iep := r.Api.GetEp("initiators")

	// Check if initiator exists
	init, err := iep.GetEp(initiator).Get(ctxt)

	var path string
	if err != nil {
		// Create the initiator
		iname, _ := dsdk.NewUUID()
		iname = Prefix + iname
		_, err = iep.Create(ctxt, fmt.Sprintf("name=%s", iname), fmt.Sprintf("id=%s", initiator))
		path = fmt.Sprintf("/initiators/%s", initiator)
	} else {
		path = init.GetM()["path"].(string)
	}

	// Register initiator with storage instance
	myInit := dsdk.Initiator{
		Path: path,
	}

	// Update existing AclPolicy if it exists
	aclep := r.Api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName).GetEp("acl_policy")
	eacl, err := aclep.Get(ctxt)
	if err != nil {
		co.Error(ctxt, err)
		return err
	}
	acl, err := dsdk.NewAclPolicy(eacl.GetB())
	if err != nil {
		co.Error(ctxt, err)
		return err
	}
	// Add the new initiator to the initiator list
	newit := append(*acl.Initiators, myInit)
	acl.Initiators = &newit
	acl.Path = ""

	_, err = aclep.Set(ctxt, acl)
	if err != nil {
		return err
	}
	return nil
}

func (r Client) DetachVolume(ctxt context.Context, name string) error {
	co.Debugf(ctxt, "DetachVolume invoked for %s", name)
	name = getName(name)

	aiep := r.Api.GetEp("app_instances").GetEp(name)
	_, err := aiep.Set(ctxt, "admin_state=offline", "force=true")
	if err != nil {
		co.Info(ctxt, err)
		return err
	}

	return nil
}

func (r Client) DeleteVolume(ctxt context.Context, name, mountpoint string) error {
	co.Debugf(ctxt, "DeleteVolume invoked for %s", name)
	name = getName(name)

	err := r.DetachVolume(ctxt, name)
	if err != nil {
		co.Debug(ctxt, err)
		return err
	}

	aiep, err := r.Api.GetEp("app_instances").GetEp(name).Get(ctxt)
	// If we don't find the app_instance, fail quietly
	if err != nil {
		co.Debugf(ctxt, "Could not find app_instance %s", name)
		return nil
	}
	err = aiep.Delete(ctxt)
	if err != nil {
		co.Debug(ctxt, err)
		return nil
	}

	return nil
}

func (r Client) GetIQNandPortals(ctxt context.Context, name string) (string, []string, string, error) {
	co.Debugf(ctxt, "GetIQNandPortals invoked for: %s", name)
	name = getName(name)

	si, err := r.Api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName).Get(ctxt)
	if err != nil {
		co.Debugf(ctxt, "Couldn't find target, Error: %s", err)
		return "", []string{}, "", err
	}

	mySi, err := dsdk.NewStorageInstance(si.GetB())
	if err != nil {
		co.Debugf(ctxt, "Couldn't unpack storage instance, Error: %s", err)
		return "", []string{}, "", err
	}
	volUUID := (*mySi.Volumes)[0].Uuid

	ips := mySi.Access["ips"].([]interface{})

	if len(ips) < 1 {
		return "", []string{}, "", fmt.Errorf("No IPs available for volume: %s", name)
	}
	var portals []string
	for _, portal := range ips {
		portals = append(portals, portal.(string))
	}
	if _, ok := mySi.Access["iqn"]; !ok {
		return "", []string{}, "", fmt.Errorf("No IQN available for volume: %s", name)
	}
	iqn := mySi.Access["iqn"].(string)

	co.Debugf(ctxt, "iqn: %s, portals: %s, volume-uuid: %s", iqn, portals, volUUID)
	return iqn, portals, volUUID, err
}

func (r Client) FindDeviceFsType(ctxt context.Context, diskPath string) (string, error) {
	co.Debugf(ctxt, "FindDeviceFsType invoked, diskPath %s", diskPath)

	var out []byte
	var err error
	if out, err = co.ExecC(ctxt, "blkid", diskPath).CombinedOutput(); err != nil {
		co.Debugf(ctxt, "Error finding FsType: %s, out: %s", err, out)
		return "", err
	}
	re, _ := regexp.Compile(`TYPE="(.*)"`)
	f := re.FindSubmatch(out)
	if len(f) > 1 {
		co.Debugf(ctxt, "Found FsType: %s for Device: %s", string(f[1]), diskPath)
		return string(f[1]), nil
	}
	return "", fmt.Errorf("Couldn't find FsType")
}

func (r Client) OnlineVolume(ctxt context.Context, name string) error {
	co.Debugf(ctxt, "OnlineVolume invoked for: %s", name)
	name = getName(name)
	aiep := r.Api.GetEp("app_instances").GetEp(name)
	ai, err := aiep.Set(ctxt, "admin_state=online")
	if err != nil {
		co.Debugf(ctxt, "Couldn't find AppInstance, Error: %s", err)
		return err
	}
	timeout := 10
	for {
		ai, err = aiep.Get(ctxt)
		myAi, err := dsdk.NewAppInstance(ai.GetB())
		if err != nil {
			co.Debugf(ctxt, "Couldn't unpack AppInstance, Error: %s", err)
			return err
		}
		if myAi.AdminState == "online" {
			break
		}
		if timeout <= 0 {
			err = fmt.Errorf("AppInstance %s never came online", name)
			co.Error(ctxt, err)
			return err
		}
		timeout--
	}
	return nil
}

func (r Client) LoginVolume(ctxt context.Context, name string, destination string) (string, error) {
	co.Debugf(ctxt, "LoginVolume invoked for: %s", name)
	name = getName(name)
	if err := r.OnlineVolume(ctxt, name); err != nil {
		return "", err
	}
	fi, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(destination, 0755); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if fi != nil && !fi.IsDir() {
		return "", fmt.Errorf("%s already exist and it's not a directory", destination)
	}
	var (
		timeout = 10
		iqn     string
		portals []string
		uuid    string
	)
	for {
		iqn, portals, uuid, err = r.GetIQNandPortals(ctxt, name)
		if err != nil {
			if timeout <= 0 {
				co.Errorf(ctxt, "Unable to find IQN and portal for %s.", name)
				return "", err
			} else {
				timeout--
				time.Sleep(time.Second)
			}
		} else {
			break
		}
	}
	// Make sure we're authorized to access the volume
	err = r.CreateACL(ctxt, name, false)
	if err != nil {
		co.Error(ctxt, err)
		return "", err
	}

	var diskPath string
	if isMultipathEnabled(ctxt) {
		timeout = 10
		if diskPath, err = loginPoller(ctxt, name, portals, iqn, uuid, timeout, true); err != nil {
			return "", err
		}
	} else {
		timeout = 10
		if diskPath, err = loginPoller(ctxt, name, portals, iqn, uuid, timeout, false); err != nil {
			return "", err
		}
	}

	return diskPath, nil

}

func (r Client) MountVolume(ctxt context.Context, name, destination, fsType, diskPath string) error {
	co.Debugf(ctxt, "MountVolume invoked for: %s, destination: %s, fsType: %s, diskPath: %s", name, destination, fsType, diskPath)
	// wait for disk to be available after target login
	name = getName(name)

	diskAvailable := waitForDisk(ctxt, diskPath, 10)
	if !diskAvailable {
		err := fmt.Errorf("Device: %s is not available in 10 seconds", diskPath)
		co.Error(ctxt, err)
		return err
	}

	mounted, err := isAlreadyMounted(ctxt, destination)
	if mounted {
		co.Errorf(ctxt, "destination mount-point: %s is in use already", destination)
		return err
	}

	// Mount the disk now to the destination
	if err := os.MkdirAll(destination, 0750); err != nil {
		co.Errorf(ctxt, "failed to create destination directory: %s", destination)
		return err
	}

	err = doMount(ctxt, diskPath, destination, fsType, nil)
	if err != nil {
		co.Errorf(ctxt, "Unable to mount iscsi volume: %s to directory: %s.", diskPath, destination)
		return err
	}

	return nil
}

func (r Client) UnmountVolume(ctxt context.Context, name string, destination string) error {
	co.Debugf(ctxt, "UnmountVolume invoked for: %s, destination: %s", name, destination)
	name = getName(name)
	iqn, portals, uuid, err := r.GetIQNandPortals(ctxt, name)
	if err != nil {
		co.Errorf(ctxt, "UnmountVolume:: Unable to find IQN and portal for: %s.", name)
		return err
	}

	err = doUnmount(ctxt, destination, 20)
	if err != nil {
		co.Errorf(ctxt, "Unable to unmount: %s", destination)
		return err
	}

	for _, portal := range portals {
		doLogout(ctxt, uuid, portal, iqn)
	}

	co.Debug(ctxt, "UnmountVolume: iscsi session logout successful.")

	initiator, err := getInitName(ctxt)
	if err != nil {
		co.Error(ctxt, err)
		return err
	}
	// Remove entry from ACL (in case another node is also attached)
	aclep := r.Api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName).GetEp("acl_policy")
	eacl, err := aclep.Get(ctxt)
	if err != nil {
		co.Info(ctxt, err)
		return err
	}
	acl, err := dsdk.NewAclPolicy(eacl.GetB())
	if err != nil {
		co.Info(ctxt, err)
		return err
	}
	var newinits []dsdk.Initiator
	path := fmt.Sprintf("/initiators/%s", initiator)
	for i, init := range *acl.Initiators {
		if init.Path == path {
			newinits = append((*acl.Initiators)[:i], (*acl.Initiators)[i+1:]...)
			break
		}
	}
	acl.Initiators = &newinits
	acl.Path = ""
	_, err = aclep.Set(ctxt, acl)
	if err != nil {
		return err
	}

	return nil
}

func (r Client) ListVolumes(ctxt context.Context) ([]string, error) {

	vNames := []string{}
	ais, err := r.Api.GetEp("app_instances").List(ctxt)
	if err != nil {
		return vNames, nil
	}

	for _, ai := range ais {
		myAi, err := dsdk.NewAppInstance(ai.GetB())
		if err != nil {
			return vNames, err
		}
		if strings.HasPrefix(myAi.Name, Prefix) {
			vNames = append(vNames, stripName(myAi.Name))
		}
	}
	co.Debugf(ctxt, "Found volumes: %s", vNames)

	return vNames, nil
}

func getMultipathDisk(ctxt context.Context, path string) (string, error) {
	// Follow link to destination directory
	device_path, err := os.Readlink(path)
	if err != nil {
		co.Errorf(ctxt, "Error reading link: %s -- error: %s", path, err)
		return "", err
	}
	sdevice := filepath.Base(device_path)
	// If destination directory is already identified as a multipath device,
	// just return its path
	if strings.HasPrefix(sdevice, "dm-") {
		return path, nil
	}
	// Fallback to iterating through all the entries under /sys/block/dm-* and
	// check to see if any have an entry under /sys/block/dm-*/slaves matching
	// the device the symlink was pointing at
	dmpaths, _ := filepath.Glob("/sys/block/dm-*")
	for _, dmpath := range dmpaths {
		sdevices, _ := filepath.Glob(filepath.Join(dmpath, "slaves", "*"))
		for _, spath := range sdevices {
			s := filepath.Base(spath)
			if sdevice == s {
				// We've found a matching entry, return the path for the
				// dm-* device it was found under
				p := filepath.Join("/dev", filepath.Base(dmpath))
				co.Debugf(ctxt, "Found matching device: %s under dm-* device path %s", sdevice, dmpath)
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("Couldn't find dm-* path for path: %s, found non dm-* path: %s", path, device_path)
}

// Returns path to block device
func doLogin(ctxt context.Context, name string, portals []string, iqn, uuid string, multipath bool) (string, error) {
	co.Debugf(ctxt, "Logging in volume: %s, iqn: %s, portals: %s", name, iqn, portals)
	var (
		diskPath string
		err      error
	)
	uuidPath := fmt.Sprintf("/dev/disk/by-uuid/%s", uuid)
	diskAvailable := waitForDisk(ctxt, uuidPath, 1)

	if diskAvailable {
		if multipath {
			diskPath, err = getMultipathDisk(ctxt, uuidPath)
			if err != nil {
				return diskPath, err
			}
		} else {
			diskPath = uuidPath
		}
		co.Debugf(ctxt, "Disk: %s is already available.", diskPath)
		return diskPath, nil
	}
	usePortals := portals

	// Only use the first portal unless we're using multipath
	if !multipath {
		usePortals = []string{portals[0]}
		co.Debugf(ctxt, "No multipath so only using first portal: %s", usePortals)
	}

	for _, portal := range usePortals {
		if out, err :=
			co.ExecC(ctxt, "iscsiadm", "-m", "discovery", "-t", "sendtargets", "-p", portal+":3260").CombinedOutput(); err != nil {
			co.Debugf(ctxt, "Unable to discover targets at portal: %s. Error output: %s", portal, string(out))
			return diskPath, err
		}

		if out, err :=
			co.ExecC(ctxt, "iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--login").CombinedOutput(); err != nil {
			co.Debugf(ctxt, "Unable to login to target: %s at portal: %s. Error output: %s",
				iqn,
				portal,
				string(out))
			return diskPath, err
		}
	}
	if multipath {
		diskPath, err = getMultipathDisk(ctxt, uuidPath)
		if err != nil {
			return diskPath, err
		}
	} else {
		diskPath = uuidPath
	}
	return diskPath, nil
}

func doMount(ctxt context.Context, sourceDisk string, destination string, fsType string, mountOptions []string) error {
	co.Debugf(ctxt, "Mounting volume: %s to: %s, file-system: %s options: %v",
		sourceDisk,
		destination,
		fsType,
		mountOptions)

	co.ExecC(ctxt, "fsck", "-a", sourceDisk).CombinedOutput()

	if out, err :=
		co.ExecC(ctxt, "mount", "-t", fsType,
			"-o", strings.Join(mountOptions, ","), sourceDisk, destination).CombinedOutput(); err != nil {
		co.Warningf(ctxt, "mount failed for volume: %s. output: %s, error: %s", sourceDisk, string(out), err)
		co.Infof(ctxt, "Checking for disk formatting: %s", sourceDisk)

		if fsType == "ext4" {
			co.Debugf(ctxt, "ext4 block fsType: %s", fsType)
			_, err =
				co.ExecC(ctxt, "mkfs."+fsType, "-E",
					"lazy_itable_init=0,lazy_journal_init=0,nodiscard", "-F", sourceDisk).CombinedOutput()
		} else if fsType == "xfs" {
			co.Debugf(ctxt, "fsType: %s", fsType)
			_, err =
				co.ExecC(ctxt, "mkfs."+fsType, "-K", sourceDisk).CombinedOutput()
		} else {
			co.Debugf(ctxt, "fsType: %s", fsType)
			_, err =
				co.ExecC(ctxt, "mkfs."+fsType, sourceDisk).CombinedOutput()
		}
		if err == nil {
			co.Debug(ctxt, "Done with formatting, mounting again.")
			if _, err := co.ExecC(ctxt, "mount", "-t", fsType,
				"-o", strings.Join(mountOptions, ","),
				sourceDisk, destination).CombinedOutput(); err != nil {
				co.Errorf(ctxt, "Error in mounting. Error: %s", err)
				return err
			} else {
				co.Debugf(ctxt, "Mounted: %s successfully on: %s", sourceDisk, destination)
				return nil
			}
		} else {
			co.Errorf(ctxt, "mkfs failed. Error: %s", err)
		}
		return err
	}
	co.Debugf(ctxt, "Mounted: successfully on: %s", sourceDisk, destination)
	return nil
}

func doUnmount(ctxt context.Context, destination string, retries int) error {
	co.Debugf(ctxt, "Unmounting: %s", destination)

	var err error
	for i := 0; i < retries; i++ {
		if out, err := co.ExecC(ctxt, "umount", destination).CombinedOutput(); err != nil {
			co.Debugf(ctxt, "doUnmount:: Unmounting failed for: %s. output: %s, error %s", destination, out, err)
			if strings.Contains(string(out), "not mounted") || strings.Contains(string(out), "not currently mounted") {
				err = nil
				break
			}
			time.Sleep(time.Second)
		} else {
			break
		}
	}

	if err != nil {
		co.Errorf(ctxt, "Could not unmount %s within %d seconds, error: %s", destination, retries, err)
		return err
	}

	if _, err = co.ExecC(ctxt, "rmdir", destination).CombinedOutput(); err != nil {
		co.Warningf(ctxt, "Couldn't remove directory: %s, err: %s", destination, err)
	}

	co.Debug(ctxt, "Unmount successful.")

	return nil
}

func isMultipathEnabled(ctxt context.Context) bool {
	cmd := "ps -ef | grep multipathd | grep -v grep | wc -l"
	if out, err := co.ExecC(ctxt, "bash", "-c", cmd).CombinedOutput(); err != nil {
		co.Debug(ctxt, "Host does not support multipathing.")
		return false
	} else {
		stringOutput := string(out[0])
		co.Debugf(ctxt, "Multipathing: output for multipath check: %s", string(out[0]))
		mpProcessCnt, _ := strconv.ParseUint(stringOutput, 10, 64)
		if mpProcessCnt != 0 {
			return true
		} else {
			return false
		}
	}
	co.Debug(ctxt, "No multipathd command found. Presume no multipathing on this node.")
	return false
}

func loginPoller(ctxt context.Context, name string, portals []string, iqn, uuid string, timeout int, multipath bool) (string, error) {
	var (
		diskPath string
		err      error
	)
	for {
		co.Debugf(ctxt, "Polling login.  Timeout %ss", timeout)
		diskPath, err = doLogin(ctxt, name, portals, iqn, uuid, multipath)
		if err != nil {
			if timeout <= 0 {
				return diskPath, err
			} else {
				timeout--
				time.Sleep(time.Second)
			}
		} else {
			break
		}
	}
	return diskPath, nil
}

// Idea is to check if destination mount point is already mounted.
// If the destination directory and its parent, both are not on the same
// device, it means directory is already mounted for something.

func isAlreadyMounted(ctxt context.Context, destination string) (bool, error) {
	destStat, err := os.Stat(destination)
	if err != nil {
		return false, err
	}

	parentDirStat, err := os.Lstat(destination + "/..")
	if err != nil {
		return false, err
	}

	if destStat.Sys().(*syscall.Stat_t).Dev != parentDirStat.Sys().(*syscall.Stat_t).Dev {
		return true, nil
	}

	return false, nil
}

func waitForDisk(ctxt context.Context, diskPath string, retries int) bool {
	for i := 0; i < retries; i++ {
		_, err := os.Stat(diskPath)
		if err == nil {
			co.Debugf(ctxt, "Disk Available: %s", diskPath)
			return true
		}

		if err != nil && !os.IsNotExist(err) {
			co.Error(ctxt, err)
			return false
		}
		co.Debugf(ctxt, "Waiting for disk: %s", err)
		time.Sleep(time.Second)
	}
	return false
}

// Doesn't return an error because we should always just log and continue
func doLogout(ctxt context.Context, uuid, portal, iqn string) {
	uuidPath := fmt.Sprintf("/dev/disk/by-uuid/%s", uuid)
	diskPath, _ := getMultipathDisk(ctxt, uuidPath)
	if out, err :=
		co.ExecC(ctxt, "iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--logout").CombinedOutput(); err != nil {
		co.Errorf(ctxt, "Unable to logout target: %s at portal: %s. Error output: %s",
			iqn,
			portal,
			string(out))
	}
	if out, err :=
		co.ExecC(ctxt, "iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--op=delete").CombinedOutput(); err != nil {
		co.Errorf(ctxt, "Unable to delete target: %s at portal: %s. Error output: %s",
			iqn,
			portal,
			string(out))
	}
	if diskPath != "" {
		disk := filepath.Base(diskPath)
		if out, err := co.ExecC(ctxt, "multipath", "-f", disk).CombinedOutput(); err != nil {
			co.Errorf(ctxt, "Unable to flush multipath device: %s", disk, string(out))
		}
	}
}

func getName(name string) string {
	if strings.HasPrefix(name, Prefix) {
		return name
	}
	return Prefix + name
}

func stripName(name string) string {
	return strings.TrimLeft(name, Prefix)
}

func getInitName(ctxt context.Context) (string, error) {
	// Parse InitiatorName
	dat, err := ioutil.ReadFile(initiatorFile)
	if err != nil {
		co.Debugf(ctxt, "Could not read file %s", initiatorFile)
		return "", err
	}
	initiator := strings.Split(strings.TrimSpace(string(dat)), "=")[1]
	co.Debugf(ctxt, initiator)

	return initiator, nil
}
