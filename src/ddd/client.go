package main

import (
	"fmt"
	"regexp"
	"strings"
	"syscall"
	"time"

	dsdk "github.com/Datera/go-sdk/src/dsdk"
	log "github.com/sirupsen/logrus"
)

const (
	VERSION       = "2.1.2"
	initiatorFile = "/etc/iscsi/initiatorname.iscsi"
	rBytes        = "0123456789abcdef"
	StorageName   = "storage-1"
	VolumeName    = "volume-1"
	IGPrefix      = "Docker-Driver-"
	DatabaseFile  = ".clientdb"
)

type Client struct {
	Debug bool
	Api   *dsdk.SDK
}

func NewClient(addr, username, password, tenant string, debug, ssl bool, driver, version string) *Client {
	headers := make(map[string]string)
	Api, err := dsdk.NewSDK(addr, username, password, "2.1", tenant, "30s", headers, false, "ddd.log", true)
	panicErr(err)
	prepareDB()
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
		at := dsdk.AppTemplate{
			Name: template,
		}
		ai = dsdk.AppInstance{
			Name:        name,
			AppTemplate: &at,
		}
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
	dat, err := FileReader(initiatorFile)
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

func (r Client) GetIQNandPortal(name string) (string, string, string, error) {
	log.Debugf("GetIQNandPortal invoked for: %s", name)

	si, err := r.Api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName).Get()
	if err != nil {
		log.Debugf("Couldn't find target, Error: %s", err)
		return "", "", "", err
	}

	mySi, err := dsdk.NewStorageInstance(si.GetB())
	if err != nil {
		log.Debugf("Couldn't unpack storage instance, Error: %s", err)
		return "", "", "", err
	}
	volUUID := (*mySi.Volumes)[0].Uuid

	ips := mySi.Access["ips"].([]interface{})

	if len(ips) < 1 {
		return "", "", "", fmt.Errorf("No IPs available for volume: %s", name)
	}
	portal := ips[0].(string)
	iqn := mySi.Access["iqn"].(string)

	log.Debugf("iqn: %s, portal: %s, volume-uuid: %s", iqn, portal, volUUID)
	return iqn, portal, volUUID, err
}

func (r Client) FindDeviceFsType(diskPath string) (string, error) {
	log.Debug("FindDeviceFsType invoked")

	var out []byte
	var err error
	if out, err = ExecC("blkid", diskPath).CombinedOutput(); err != nil {
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
	fi, err := OS.Lstat(destination)
	if OS.IsNotExist(err) {
		if err := OS.MkdirAll(destination, 0755); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	if fi != nil && !fi.IsDir() {
		return "", fmt.Errorf("%s already exist and it's not a directory", destination)
	}
	iqn, portal, _, err := r.GetIQNandPortal(name)
	if err != nil {
		log.Debugf("Unable to find IQN and portal for %s.", name)
		return "", err
	}
	// Make sure we're authorized to access the volume
	err = r.CreateACL(name, false)
	if err != nil {
		log.Error(err)
		return "", err
	}

	timeout := 10
	var diskPath string
	for {
		diskPath, err = doLogin(name, portal, iqn)
		if err != nil {
			if timeout <= 0 {
				return "", err
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

// TODO(_alastor_): Eventually allow non 0 index luns
func buildPath(portal, iqn string) string {
	return fmt.Sprintf("/dev/disk/by-path/ip-%s:3260-iscsi-%s-lun-0", portal, iqn)
}

// Returns path to block device
func doLogin(name, portal, iqn string) (string, error) {
	log.Debugf("Logging in volume %s", name)
	diskPath := buildPath(portal, iqn)
	diskAvailable := waitForDisk(diskPath, 1)

	if diskAvailable {
		log.Debugf("Disk: %s is already available.", diskPath)
		return diskPath, nil
	}

	if out, err :=
		ExecC("iscsiadm", "-m", "discovery", "-t", "sendtargets", "-p", portal+":3260").CombinedOutput(); err != nil {
		log.Debugf("Unable to discover targets at portal: %s. Error output: %s", portal, string(out))
		return "", err
	}

	if out, err :=
		ExecC("iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--login").CombinedOutput(); err != nil {
		log.Debugf("Unable to login to target: %s at portal: %s. Error output: %s",
			iqn,
			portal,
			string(out))
		return "", err
	}
	return diskPath, nil
}

func (r Client) MountVolume(name, destination, fsType, diskPath string) error {
	// wait for disk to be available after target login

	diskAvailable := waitForDisk(diskPath, 10)
	if !diskAvailable {
		err := fmt.Errorf("Device: %s is not available in 10 seconds", diskPath)
		log.Error(err)
		return err
	}

	mounted, err := IsAlreadyMounted(destination)
	if mounted {
		log.Errorf("destination mount-point: %s is in use already", destination)
		return err
	}

	// Mount the disk now to the destination
	if err := OS.MkdirAll(destination, 0750); err != nil {
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

func doMount(sourceDisk string, destination string, fsType string, mountOptions []string) error {
	log.Debugf("Mounting volume: %s to: %s, file-system: %s options: %v",
		sourceDisk,
		destination,
		fsType,
		mountOptions)

	ExecC("fsck", "-a", sourceDisk).CombinedOutput()

	if out, err :=
		ExecC("mount", "-t", fsType,
			"-o", strings.Join(mountOptions, ","), sourceDisk, destination).CombinedOutput(); err != nil {
		log.Warningf("mount failed for volume: %s. output: %s, error: %s", sourceDisk, string(out), err)
		log.Infof("Checking for disk formatting: %s", sourceDisk)

		if fsType == "ext4" {
			log.Debugf("ext4 block fsType: %s", fsType)
			_, err =
				ExecC("mkfs."+fsType, "-E",
					"lazy_itable_init=0,lazy_journal_init=0,nodiscard", "-F", sourceDisk).CombinedOutput()
		} else if fsType == "xfs" {
			log.Debugf("fsType: %s", fsType)
			_, err =
				ExecC("mkfs."+fsType, "-K", sourceDisk).CombinedOutput()
		} else {
			log.Debugf("fsType: %s", fsType)
			_, err =
				ExecC("mkfs."+fsType, sourceDisk).CombinedOutput()
		}
		if err == nil {
			log.Debug("Done with formatting, mounting again.")
			if _, err := ExecC("mount", "-t", fsType,
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
		if out, err := ExecC("umount", destination).CombinedOutput(); err != nil {
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

	if _, err = ExecC("rmdir", destination).CombinedOutput(); err != nil {
		log.Warningf("Couldn't remove directory: %s, err: %s", destination, err)
	}

	log.Debug("Unmount successful.")

	return nil
}

func waitForDisk(diskPath string, retries int) bool {
	for i := 0; i < retries; i++ {
		_, err := OS.Stat(diskPath)
		if err == nil {
			log.Debugf("Disk Available: %s", diskPath)
			return true
		}

		if err != nil && !OS.IsNotExist(err) {
			log.Error(err)
			return false
		}
		log.Debugf("Waiting for disk: %s", err)
		time.Sleep(time.Second)
	}
	return false
}

// Idea is to check if destination mount point is already mounted.
// If the destination directory and its parent, both are not on the same
// device, it means directory is already mounted for something.

// Test overridable testvariable
var IsAlreadyMounted = _isAlreadyMounted

func _isAlreadyMounted(destination string) (bool, error) {
	destStat, err := OS.Stat(destination)
	if err != nil {
		return false, err
	}

	parentDirStat, err := OS.Lstat(destination + "/..")
	if err != nil {
		return false, err
	}

	if destStat.Sys().(*syscall.Stat_t).Dev != parentDirStat.Sys().(*syscall.Stat_t).Dev {
		return true, nil
	}

	return false, nil
}

func (r Client) UnmountVolume(name string, destination string) error {
	iqn, portal, _, err := r.GetIQNandPortal(name)
	if err != nil {
		log.Errorf("UnmountVolume:: Unable to find IQN and portal for: %s.", name)
		return err
	}

	err = doUnmount(destination, 20)
	if err != nil {
		log.Errorf("Unable to unmount: %s", destination)
		return err
	}

	if out, err :=
		ExecC("iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--logout").CombinedOutput(); err != nil {
		log.Errorf("Unable to logout target: %s at portal: %s. Error output: %s",
			iqn,
			portal,
			string(out))
		return err
	}

	log.Debug("UnmountVolume: iscsi session logout successful.")

	return nil
}
