package client

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	co "ddd/common"
	dsdk "github.com/Datera/go-sdk/src/dsdk"
	log "github.com/sirupsen/logrus"
)

const (
	initiatorFile = "/etc/iscsi/initiatorname.iscsi"
	rBytes        = "0123456789abcdef"
	StorageName   = "storage-1"
	VolumeName    = "volume-1"
	IGPrefix      = "Docker-Driver-"
)

type Client struct {
	Debug bool
	Api   *dsdk.SDK
}

func NewClient(addr, username, password, tenant string, debug, ssl bool, driver, version string) *Client {
	headers := make(map[string]string)
	Api, err := dsdk.NewSDK(addr, username, password, "2.1", tenant, "30s", headers, false, "ddd.log", true)
	co.PanicErr(err)
	co.PrepareDB()
	client := &Client{
		Api:   Api,
		Debug: debug,
	}
	log.Debugf("Client: %#v", client)
	return client
}

func (r Client) VolumeExist(name string) (bool, error) {
	log.Debugf("VolumeExist invoked for %s", name)
	_, err := r.Api.GetEp("app_instances").GetEp(name).Get()
	if err != nil {
		log.Debugf("Volume %s not found", name)
		return false, err
	}
	log.Debugf("Volume %s found", name)
	return true, nil
}

func (r Client) CreateVolume(name string, size int, replica int, template string, maxIops int, maxBW int, placementMode string) error {
	log.Debugf("CreateVolume invoked for %s, size %d, replica %d, template %s, maxIops %d, maxBW %d, placementMode %s",
		name, size, replica, template, maxIops, maxBW, placementMode)
	var ai dsdk.AppInstance
	if template != "" {
		template = strings.Trim(template, "/")
		log.Debugf("Creating AppInstance with template: %s", template)
		at := dsdk.AppTemplate{
			Path: "/app_templates/" + template,
		}
		ai = dsdk.AppInstance{
			Name:        name,
			AppTemplate: &at,
		}
		log.Debugf("AI: %#v", ai)
	} else {
		vol := dsdk.Volume{
			Name:          VolumeName,
			Size:          float64(size),
			PlacementMode: placementMode,
			ReplicaCount:  replica,
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
	_, err := r.Api.GetEp("app_instances").Create(ai)
	if err != nil {
		log.Error(err)
		return err
	}
	// Handle QoS values
	if maxIops != 0 || maxBW != 0 {
		pp := dsdk.PerformancePolicy{
			TotalIopsMax:      maxIops,
			TotalBandwidthMax: maxBW,
		}
		// Get Performance_policy endpoint
		_, err = r.Api.GetEp("app_instances").GetEp(name).GetEp(
			"storage_instances").GetEp(StorageName).GetEp(
			"volumes").GetEp(VolumeName).GetEp(
			"performance_policy").Create(pp)
	}

	return nil
}

func (r Client) CreateACL(name string, random bool) error {
	log.Debugf("CreateACL invoked for %s", name)
	// Parse InitiatorName
	dat, err := co.FileReader(initiatorFile)
	if err != nil {
		log.Debugf("Could not read file %s", initiatorFile)
		return err
	}
	initiator := strings.Split(strings.TrimSpace(string(dat)), "=")[1]
	log.Debugf(initiator)

	iep := r.Api.GetEp("initiators")

	// Check if initiator exists
	init, err := iep.GetEp(initiator).Get()

	var path string
	if err != nil {
		// Create the initiator
		iname, _ := dsdk.NewUUID()
		iname = IGPrefix + iname
		_, err = iep.Create(fmt.Sprintf("name=%s", iname), fmt.Sprintf("id=%s", initiator))
		path = fmt.Sprintf("/initiators/%s", initiator)
	} else {
		path = init.GetM()["path"].(string)
	}

	// Register initiator with storage instance
	myInit := dsdk.Initiator{
		Path: path,
	}
	aclp := dsdk.AclPolicy{
		Initiators: &[]dsdk.Initiator{myInit},
	}
	aclep := r.Api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName).GetEp("acl_policy")
	_, err = aclep.Set(aclp)
	if err != nil {
		return err
	}
	return nil
}

func (r Client) DetachVolume(name string) error {
	log.Debugf("DetachVolume invoked for %s", name)

	siep := r.Api.GetEp("app_instances").GetEp(name)
	_, err := siep.Set("admin_state=offline", "force=true")
	if err != nil {
		return err
	}

	return nil
}

func (r Client) DeleteVolume(name, mountpoint string) error {
	log.Debugf("DeleteVolume invoked for %s", name)

	err := r.DetachVolume(name)
	if err != nil {
		log.Debug(err)
		return err
	}

	aiep, err := r.Api.GetEp("app_instances").GetEp(name).Get()
	// If we don't find the app_instance, fail quietly
	if err != nil {
		log.Debugf("Could not find app_instance %s", name)
		return nil
	}
	err = aiep.Delete()
	if err != nil {
		log.Debug(err)
		return nil
	}

	return nil
}

func (r Client) GetIQNandPortals(name string) (string, []string, string, error) {
	log.Debugf("GetIQNandPortals invoked for: %s", name)

	si, err := r.Api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName).Get()
	if err != nil {
		log.Debugf("Couldn't find target, Error: %s", err)
		return "", []string{}, "", err
	}

	mySi, err := dsdk.NewStorageInstance(si.GetB())
	if err != nil {
		log.Debugf("Couldn't unpack storage instance, Error: %s", err)
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

	log.Debugf("iqn: %s, portals: %s, volume-uuid: %s", iqn, portals, volUUID)
	return iqn, portals, volUUID, err
}

func (r Client) FindDeviceFsType(diskPath string) (string, error) {
	log.Debug("FindDeviceFsType invoked")

	var out []byte
	var err error
	if out, err = co.ExecC("blkid", diskPath).CombinedOutput(); err != nil {
		log.Debugf("Error finding FsType: %s, out: %s", err, out)
		return "", err
	}
	re, _ := regexp.Compile(`TYPE="(.*)"`)
	f := re.FindSubmatch(out)
	if len(f) > 1 {
		log.Debugf("Found FsType: %s for Device: %s", string(f[1]), diskPath)
		return string(f[1]), nil
	}
	return "", fmt.Errorf("Couldn't find FsType")
}

func (r Client) LoginVolume(name string, destination string) (string, error) {
	log.Debugf("LoginVolume invoked for: %s", name)
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
		iqn, portals, uuid, err = r.GetIQNandPortals(name)
		if err != nil {
			if timeout <= 0 {
				log.Debugf("Unable to find IQN and portal for %s.", name)
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
	err = r.CreateACL(name, false)
	if err != nil {
		log.Error(err)
		return "", err
	}

	var diskPath string
	if isMultipathEnabled() {
		for _, portal := range portals {
			timeout = 10
			if diskPath, err = loginPoller(name, portal, iqn, uuid, timeout, true); err != nil {
				return "", err
			}
		}
	} else {
		timeout = 10
		if diskPath, err = loginPoller(name, portals[0], iqn, uuid, timeout, false); err != nil {
			return "", err
		}
	}

	return diskPath, nil

}

func (r Client) MountVolume(name, destination, fsType, diskPath string) error {
	log.Debugf("MountVolume invoked for: %s, destination: %s, fsType: %s, diskPath: %s", name, destination, fsType, diskPath)
	// wait for disk to be available after target login

	diskAvailable := waitForDisk(diskPath, 10)
	if !diskAvailable {
		err := fmt.Errorf("Device: %s is not available in 10 seconds", diskPath)
		log.Error(err)
		return err
	}

	mounted, err := isAlreadyMounted(destination)
	if mounted {
		log.Errorf("destination mount-point: %s is in use already", destination)
		return err
	}

	// Mount the disk now to the destination
	if err := os.MkdirAll(destination, 0750); err != nil {
		log.Errorf("failed to create destination directory: %s", destination)
		return err
	}

	err = doMount(diskPath, destination, fsType, nil)
	if err != nil {
		log.Errorf("Unable to mount iscsi volume: %s to directory: %s.", diskPath, destination)
		return err
	}

	return nil
}

func (r Client) UnmountVolume(name string, destination string) error {
	iqn, portals, _, err := r.GetIQNandPortals(name)
	if err != nil {
		log.Errorf("UnmountVolume:: Unable to find IQN and portal for: %s.", name)
		return err
	}

	err = doUnmount(destination, 20)
	if err != nil {
		log.Errorf("Unable to unmount: %s", destination)
		return err
	}

	for _, portal := range portals {
		if err = logoutCommand(portal, iqn); err != nil {
			// Failed logouts shouldn't return an error, but we should
			// log it to make sure we know the reason
			log.Warning(err)
		}
	}

	log.Debug("UnmountVolume: iscsi session logout successful.")

	return nil
}

func getMultipathDisk(path string) (string, error) {
	// Follow link to destination directory
	device_path, err := os.Readlink(path)
	if err != nil {
		log.Errorf("Error reading link: %s -- error: %s", path, err)
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
				log.Debugf("Found matching device: %s under dm-* device path %s", sdevice, dmpath)
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("Couldn't find dm-* path for path: %s, found non dm-* path: %s", path, device_path)
}

// Returns path to block device
func doLogin(name, portal, iqn, uuid string, multipath bool) (string, error) {
	log.Debugf("Logging in volume: %s, iqn: %s, portal: %s", name, iqn, portal)
	var (
		diskPath string
		err      error
	)
	uuidPath := fmt.Sprintf("/dev/disk/by-uuid/%s", uuid)
	diskAvailable := waitForDisk(uuidPath, 1)

	if diskAvailable {
		if multipath {
			diskPath, err = getMultipathDisk(uuidPath)
			if err != nil {
				return diskPath, err
			}
		}
		log.Debugf("Disk: %s is already available.", diskPath)
		return diskPath, nil
	}

	if out, err :=
		co.ExecC("iscsiadm", "-m", "discovery", "-t", "sendtargets", "-p", portal+":3260").CombinedOutput(); err != nil {
		log.Debugf("Unable to discover targets at portal: %s. Error output: %s", portal, string(out))
		return diskPath, err
	}

	if out, err :=
		co.ExecC("iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--login").CombinedOutput(); err != nil {
		log.Debugf("Unable to login to target: %s at portal: %s. Error output: %s",
			iqn,
			portal,
			string(out))
		return diskPath, err
	}
	if multipath {
		diskPath, err = getMultipathDisk(uuidPath)
		if err != nil {
			return diskPath, err
		}
	}
	return diskPath, nil
}

func doMount(sourceDisk string, destination string, fsType string, mountOptions []string) error {
	log.Debugf("Mounting volume: %s to: %s, file-system: %s options: %v",
		sourceDisk,
		destination,
		fsType,
		mountOptions)

	co.ExecC("fsck", "-a", sourceDisk).CombinedOutput()

	if out, err :=
		co.ExecC("mount", "-t", fsType,
			"-o", strings.Join(mountOptions, ","), sourceDisk, destination).CombinedOutput(); err != nil {
		log.Warningf("mount failed for volume: %s. output: %s, error: %s", sourceDisk, string(out), err)
		log.Infof("Checking for disk formatting: %s", sourceDisk)

		if fsType == "ext4" {
			log.Debugf("ext4 block fsType: %s", fsType)
			_, err =
				co.ExecC("mkfs."+fsType, "-E",
					"lazy_itable_init=0,lazy_journal_init=0,nodiscard", "-F", sourceDisk).CombinedOutput()
		} else if fsType == "xfs" {
			log.Debugf("fsType: %s", fsType)
			_, err =
				co.ExecC("mkfs."+fsType, "-K", sourceDisk).CombinedOutput()
		} else {
			log.Debugf("fsType: %s", fsType)
			_, err =
				co.ExecC("mkfs."+fsType, sourceDisk).CombinedOutput()
		}
		if err == nil {
			log.Debug("Done with formatting, mounting again.")
			if _, err := co.ExecC("mount", "-t", fsType,
				"-o", strings.Join(mountOptions, ","),
				sourceDisk, destination).CombinedOutput(); err != nil {
				log.Errorf("Error in mounting. Error: %s", err)
				return err
			} else {
				log.Debugf("Mounted: %s successfully on: %s", sourceDisk, destination)
				return nil
			}
		} else {
			log.Errorf("mkfs failed. Error: %s", err)
		}
		return err
	}
	log.Debugf("Mounted: successfully on: %s", sourceDisk, destination)
	return nil
}

func doUnmount(destination string, retries int) error {
	log.Debugf("Unmounting: %s", destination)

	var err error
	for i := 0; i < retries; i++ {
		if out, err := co.ExecC("umount", destination).CombinedOutput(); err != nil {
			log.Debugf("doUnmount:: Unmounting failed for: %s. output: %s, error %s", destination, out, err)
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
		log.Errorf("Could not unmount %s within %d seconds, error: %s", destination, retries, err)
		return err
	}

	if _, err = co.ExecC("rmdir", destination).CombinedOutput(); err != nil {
		log.Warningf("Couldn't remove directory: %s, err: %s", destination, err)
	}

	log.Debug("Unmount successful.")

	return nil
}

func isMultipathEnabled() bool {
	cmd := "ps -ef | grep multipathd | grep -v grep | wc -l"
	if out, err := co.ExecC("bash", "-c", cmd).CombinedOutput(); err != nil {
		log.Debug("Host does not support multipathing.")
		return false
	} else {
		stringOutput := string(out[0])
		log.Debugf("Multipathing: output for multipath check: %s", string(out[0]))
		mpProcessCnt, _ := strconv.ParseUint(stringOutput, 10, 64)
		if mpProcessCnt != 0 {
			return true
		} else {
			return false
		}
	}
	log.Debug("No multipathd command found. Presume no multipathing on this node.")
	return false
}

func loginPoller(name, portal, iqn, uuid string, timeout int, multipath bool) (string, error) {
	var (
		diskPath string
		err      error
	)
	for {
		log.Debugf("Polling login.  Timeout %ss", timeout)
		diskPath, err = doLogin(name, portal, iqn, uuid, multipath)
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

func isAlreadyMounted(destination string) (bool, error) {
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

func waitForDisk(diskPath string, retries int) bool {
	for i := 0; i < retries; i++ {
		_, err := os.Stat(diskPath)
		if err == nil {
			log.Debugf("Disk Available: %s", diskPath)
			return true
		}

		if err != nil && !os.IsNotExist(err) {
			log.Error(err)
			return false
		}
		log.Debugf("Waiting for disk: %s", err)
		time.Sleep(time.Second)
	}
	return false
}

func logoutCommand(portal, iqn string) error {
	if out, err :=
		co.ExecC("iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--logout").CombinedOutput(); err != nil {
		log.Errorf("Unable to logout target: %s at portal: %s. Error output: %s",
			iqn,
			portal,
			string(out))
		return err
	}
	return nil
}