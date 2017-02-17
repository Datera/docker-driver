package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"syscall"
	"time"

	dsdk "dsdk"
	log "github.com/Sirupsen/logrus"
)

const (
	VERSION       = "2.1.0"
	initiatorFile = "/etc/iscsi/initiatorname.iscsi"
	StorageName   = "storage-1"
	VolumeName    = "volume-1"
)

type Client struct {
	base  string
	debug bool
	api   *dsdk.SDK
}

func NewClient(addr, base, username, password, tenant string, debug, ssl bool, driver, version string) *Client {
	headers := make(map[string]string)
	api, err := dsdk.NewSDK(addr, "7717", "2.1", username, password, tenant, "30s", headers, false)
	if err != nil {
		panic(err)
	}
	client := &Client{
		api:   api,
		base:  base,
		debug: debug,
	}
	log.Debugf("Client: %#v", client)
	return client
}

func (r Client) VolumeExist(name string) (bool, error) {
	log.Debugf("VolumeExist invoked for %s", name)
	_, err := r.api.GetEp("app_instances").GetEp(name).Get()
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r Client) CreateVolume(name string, size int, replica int, template string, maxIops int, maxBW int) error {
	log.Debugf("CreateVolume invoked for %s", name)
	// TODO(_alastor_) add QoS and Template Support
	// Currently those parameters are ignored
	vol := dsdk.Volume{
		Name:         VolumeName,
		Size:         size,
		ReplicaCount: replica,
	}
	si := dsdk.StorageInstance{
		Name:    StorageName,
		Volumes: &[]dsdk.Volume{vol},
	}
	ai := dsdk.AppInstance{
		Name:             name,
		StorageInstances: &[]dsdk.StorageInstance{si},
	}
	_, err := r.api.GetEp("app_instances").Create(ai)
	if err != nil {
		log.Error(err)
		return err
	}
	err = r.CreateACL(name)
	if err != nil {
		log.Error(err)
		return err
	}
	return nil
}

func (r Client) CreateACL(name string) error {
	log.Debugf("CreateACL invoked for %s", name)
	// Parse InitiatorName
	dat, err := ioutil.ReadFile(initiatorFile)
	if err != nil {
		log.Debugf("Could not read file %#v", initiatorFile)
	}
	initiator := strings.Split(strings.TrimSpace(string(dat)), "=")[1]
	log.Debugf(initiator)

	iep := r.api.GetEp("initiators")

	// Check if initiator exists
	init, err := iep.GetEp(initiator).Get()

	var iname string
	var path string
	if err != nil {
		// Create the initiator
		iname, _ := dsdk.NewUUID()
		_, err = iep.Create(fmt.Sprintf("name=%s", iname), fmt.Sprintf("id=%s", initiator))
		path = fmt.Sprintf("/initiators/%s", initiator)
	} else {
		iname = init.GetM()["name"].(string)
		path = init.GetM()["path"].(string)
	}

	// Register initiator with storage instance
	myInit := dsdk.Initiator{
		Name: iname,
		Path: path,
	}
	aclp := dsdk.AclPolicy{
		Initiators: &[]dsdk.Initiator{myInit},
	}
	aclep := r.api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName).GetEp("acl_policy")
	aclep.Set(aclp)
	if err != nil {
		return err
	}
	return nil
}

func (r Client) DetachVolume(name string) error {
	log.Debugf("DetachVolume invoked for %s", name)

	siep := r.api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName)
	_, err := siep.Set("admin_state=offline", "force=true")
	if err != nil {
		return err
	}

	return nil
}

func (r Client) DeleteVolume(name string) error {
	log.Debugf("DeleteVolume invoked for ", name)

	err := r.DetachVolume(name)
	if err != nil {
		return err
	}
	aiep, err := r.api.GetEp("app_instances").GetEp(name).Get()
	if err != nil {
		return err
	}
	err = aiep.Delete()
	if err != nil {
		return err
	}

	return nil
}

func (r Client) GetIQNandPortal(name string) (string, string, string, error) {
	log.Debugf("GetIQNandPortal invoked for [%s]", name)

	si, err := r.api.GetEp("app_instances").GetEp(name).GetEp("storage_instances").GetEp(StorageName).Get()
	if err != nil {
		return "", "", "", err
	}

	var mySi dsdk.StorageInstance
	err = json.Unmarshal(si.GetB(), &mySi)
	if err != nil {
		return "", "", "", err
	}
	volUUID := (*mySi.Volumes)[0].Uuid

	access := mySi.Access.(map[string]interface{})
	ips := access["ips"].([]string)
	portal := ips[0]
	iqn := access["iqn"].(string)

	log.Debugf("iqn: %s, portal: %s, volume-uuid: %s", iqn, portal, volUUID)
	return iqn, portal, volUUID, err
}

func (r Client) MountVolume(name string, destination string, fsType string) error {
	iqn, portal, volUUID, err := r.GetIQNandPortal(name)
	if err != nil {
		log.Debugf(
			fmt.Sprintf("Unable to find IQN and portal for %#v.", name))
		return err
	}

	diskPath := fmt.Sprintf("/dev/disk/by-uuid/%s", volUUID)
	diskAvailable := waitForDisk(diskPath, 1)

	if diskAvailable {
		log.Debugf("Disk: %s is already available.", diskPath)
		return nil
	}

	if out, err :=
		ExecC("iscsiadm", "-m", "discovery", "-t", "sendtargets", "-p", portal+":3260").CombinedOutput(); err != nil {
		log.Debugf("Unable to discover targets at portal: %s. Error output: %s", portal, string(out))
		return err
	}

	if out, err :=
		ExecC("iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--login").CombinedOutput(); err != nil {
		log.Debugf("Unable to login to target: %s at portal: %s. Error output: %s",
			iqn,
			portal,
			string(out))
		return err
	}

	// wait for disk to be available after target login

	diskAvailable = waitForDisk(diskPath, 10)
	if !diskAvailable {
		log.Debugf("Device: %s is not available in 10 seconds", diskPath)
		return err
	}

	mounted, err := isAlreadyMounted(destination)
	if mounted {
		log.Debugf("destination mount-point: %s is in use already", destination)
		return err
	}

	// Mount the disk now to the destination
	if err := os.MkdirAll(destination, 0750); err != nil {
		log.Debugf("failed to create destination directory: %s", destination)
		return err
	}

	err = doMount(diskPath, destination, fsType, nil)
	if err != nil {
		log.Debugf("Unable to mount iscsi volume: %s to directory: %s.", diskPath, destination)
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

func doUnmount(destination string) error {
	log.Debugf("Unmounting: %s", destination)

	if out, err := ExecC("umount", destination).CombinedOutput(); err != nil {
		log.Errorf("doUnmount:: Unmounting failed for: %s. output: %s", destination, out)
		log.Errorf("doUnmount:: error = %s", err)
		return err
	}

	log.Debug("Unmount successful.")

	return nil
}

func waitForDisk(diskPath string, retries int) bool {
	for i := 0; i < retries; i++ {
		_, err := os.Stat(diskPath)
		if err == nil {
			return true
		}

		if err != nil && !os.IsNotExist(err) {
			return false
		}

		time.Sleep(time.Second)
	}
	return false
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

func (r Client) UnmountVolume(name string, destination string) error {
	iqn, portal, _, err := r.GetIQNandPortal(name)
	if err != nil {
		log.Errorf("UnmountVolume:: Unable to find IQN and portal for: %s.", name)
		return err
	}

	err = doUnmount(destination)
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
