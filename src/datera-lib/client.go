package datera

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	volumesPath      = "/v2/app_instances"
	volumeCreatePath = "/v2/app_instances"
	volumeStopPath   = "/v2/app_instances/%s"
	volumeGetPath    = "/v2/app_instances/%s"
	loginPath        = "/v2/login"
	VERSION          = "1.0"
)

var (
	authToken string
)

type peer struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type volume struct {
	Name    string `json:"name"`
	Uuid    string `json:"uuid"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Size    int    `json:"size"`
	Replica int    `json:"replica"`
}

type response struct {
	Ok  bool   `json:"ok"`
	Err string `json:"error,omitempty"`
}

type peerResponse struct {
	Data []peer `json:"data",omitempty`
	response
}

type volumeResponse struct {
	Data []volume `json:"data",omitempty`
	response
}

type Client struct {
	addr     string
	base     string
	username string
	password string
	debug    bool
}

func NewClient(addr, base, username, password string, debug bool) *Client {
	client := &Client{addr, base, username, password, debug}
	log.Printf("Client: %#v", client)
	return client
}

func (r Client) Login(name string, password string) error {
	log.Printf("Login to [%#v] with user [%#v]", r.addr, name)
	url := fmt.Sprintf("http://%s%s", r.addr, fmt.Sprintf(loginPath))
	fmt.Println(url)

	var jsonStr = []byte(
		fmt.Sprintf(`
			    {
				"name": "%s",
				"password": "%s"
			    }`, name, password))
	authToken = ""
	resp, err := apiRequest(url, "PUT", jsonStr)
	defer resp.Body.Close()

	if err != nil {
		log.Println("Authorization failed. Check username, password or cluster IP")
		fmt.Println("Authorization failed. Check username, password or cluster IP")
		return err
	}

	contents, err := ioutil.ReadAll(resp.Body)
	var jsonResp interface{}
	err = json.Unmarshal([]byte(contents), &jsonResp)
	log.Println(
		fmt.Sprintf("Login Response:\n %#v", string(contents)))
	if err != nil {
		log.Println("Invalid response: ", jsonResp)
		fmt.Println("Invalid response: ", jsonResp)
		return err
	}

	jsonResult := jsonResp.(map[string]interface{})
	authToken = jsonResult["key"].(string)

	log.Println(
		fmt.Sprintf("AuthToken = [%s]", authToken))
	return err
}

func (r Client) VolumeExist(name string) (bool, error) {
	log.Printf("volumeExist invoked for [%#v]", name)
	vols, err := r.volumes()
	if err != nil {
		return false, err
	}

	for _, v := range vols {
		if v.Name == name {
			return true, nil
		}
	}

	return false, nil
}

func (r Client) volumes() ([]volume, error) {
	authErr := r.Login(r.username, r.password)
	if authErr != nil {
		log.Println("Authentication Failure.")
		return nil, authErr
	}
	u := fmt.Sprintf("http://%s%s", r.addr, volumesPath)

	res, err := apiRequest(u, "GET", nil)
	defer res.Body.Close()

	contents, _ := ioutil.ReadAll(res.Body)
	log.Println("response body for get-volumes:\n", string(contents))
	if err != nil {
		log.Printf("Volume list can not be fetched.")
		return nil, err
	}

	var appInstance map[string]interface{}
	if err := json.Unmarshal([]byte(contents), &appInstance); err != nil {
		log.Printf("json decoder failed for response.")
		return nil, err
	}

	var outVolumes []volume
	for k, v := range appInstance {
		if r.debug {
			log.Printf("key: ", k)
			log.Printf("Value:\n", v)
		}

		storageInstances := v.(map[string]interface{})
		storage := storageInstances["storage_instances"].(map[string]interface{})

		for k1, v1 := range storage {
			if r.debug {
				log.Printf("key1: ", k1)
				log.Printf("Value1:\n", v1)
			}

			storageInstance := v1.(map[string]interface{})

			targetUUID := storageInstance["uuid"].(string)
			if r.debug {
				log.Printf("targetUUID = ", targetUUID)
			}

			access := storageInstance["access"].(map[string]interface{})
			if r.debug {
				log.Printf("access = ", access)
			}

			storageIP := access["ips"].([]interface{})
			if r.debug {
				log.Printf("storageIP =", storageIP[0].(string))
			}

			storageIQN := access["iqn"].(string)
			if r.debug {
				log.Printf("storageIQN = ", storageIQN)
			}

			volumes := storageInstance["volumes"].(map[string]interface{})
			if r.debug {
				log.Printf("volumes = ", volumes)
			}

			for vol_key, vol_val := range volumes {
				var volumeEntry volume
				if r.debug {
					log.Printf("vol_key: ", vol_key)
					log.Printf("vol_val: ", vol_val)
				}

				volumeData := vol_val.(map[string]interface{})

				volumeName := volumeData["name"].(string)
				if r.debug {
					log.Printf("volumeName = ", volumeName)
				}
				volumeEntry.Name = volumeName

				volumeUUID := volumeData["uuid"].(string)
				if r.debug {
					log.Printf("volumeUUID = ", volumeUUID)
				}
				volumeEntry.Uuid = volumeUUID

				volumeStatus := volumeData["op_state"].(string)
				if r.debug {
					log.Printf("volumeStatus = ", volumeStatus)
				}
				volumeEntry.Status = volumeStatus

				volumeSize := volumeData["size"].(float64)
				if r.debug {
					log.Printf("volumeSize = ", volumeSize)
				}
				volumeEntry.Size = int(volumeSize)

				volumeReplica := volumeData["replica_count"].(float64)
				if r.debug {
					log.Printf("volumeReplica = ", volumeReplica)
				}
				volumeEntry.Replica = int(volumeReplica)

				outVolumes = append(outVolumes, volumeEntry)
				if r.debug {
					log.Printf("volume [", volumeEntry, "]")
				}
			}

			storage_name := storageInstance["name"].(string)
			if r.debug {
				log.Printf("storage name = ", storage_name)
			}
		}
	}

	return outVolumes, nil
}

func (r Client) CreateVolume(
	name string,
	size uint64,
	replica uint8,
	template string,
	maxIops uint64,
	maxBW uint64) error {
	authErr := r.Login(r.username, r.password)
	if authErr != nil {
		log.Println("Authentication Failure.")
		return authErr
	}

	log.Printf("template [%#v], maxIops %d, maxBW %d", template, maxIops, maxBW)
	templateUsed := false
	if len(template) != 0 {
		templateUsed = true
	}
	u := fmt.Sprintf("http://%s%s", r.addr, fmt.Sprintf(volumeCreatePath))
	fmt.Println(u)

	var jsonStr string
	if templateUsed == false {
		jsonStr =
			`{"name":"` + name + `",
			"access_control_mode":"allow_all",
			"storage_instances": {
				"storage-1": {
					"name":"storage-1",
					"volumes":{
						"` + name + `":{
						"name":"` + name + `",
						"replica_count":` + strconv.Itoa(int(replica)) + `,
						"size":` + strconv.Itoa(int(size)) + `,
						"snapshot_policies":{}
					}
				}
			}
			}
		}`
	} else {
		jsonStr =
			`{"name":"` + name + `",
			"access_control_mode":"allow_all",
			"app_template":"/app_templates/` + template + `"
		}`
	}

	log.Println("jsonStr:\n", jsonStr)
	resp, err := apiRequest(u, "POST", []byte(jsonStr))
	defer resp.Body.Close()
	if err != nil {
		return err
	}
	body, _ := ioutil.ReadAll(resp.Body)
	log.Println(
		fmt.Sprintf("response Body:\n%#v", string(body)))
	fmt.Println("response Body:", string(body))

	return responseCheck(resp)
}

func (r Client) DetachVolume(name string) error {
	log.Println("DetachVolume invoked for ", name)
	u := fmt.Sprintf("http://%s%s", r.addr, fmt.Sprintf(volumeStopPath, name))

	var jsonStr string
	jsonStr =
		`{"admin_state": "offline",
	"force": true
}`
	resp, err := apiRequest(u, "PUT", []byte(jsonStr))
	defer resp.Body.Close()
	if err != nil {
		return err
	}

	body, _ := ioutil.ReadAll(resp.Body)
	log.Println(
		fmt.Sprintf("response Body:\n%#v", string(body)))
	fmt.Println("response Body:", string(body))

	return responseCheck(resp)
}

func (r Client) StopVolume(name string) error {
	log.Println("StopVolume invoked for ", name)
	authErr := r.Login(r.username, r.password)
	if authErr != nil {
		fmt.Println("Authentication Failure.")
		return authErr
	}

	err := r.DetachVolume(name)
	u := fmt.Sprintf("http://%s%s", r.addr, fmt.Sprintf(volumeStopPath, name))

	_, err = apiRequest(u, "DELETE", nil)
	if err != nil {
		log.Println("Error in delete operation.")
		return err
	}

	//return responseCheck(resp)
	return nil
}

func (r Client) GetIQNandPortal(name string) (string, string, string, error) {
	log.Printf("GetIQNandPortal invoked for [%#v]", name)
	authErr := r.Login(r.username, r.password)
	if authErr != nil {
		fmt.Println("Authentication Failure.")
		return "", "", "", authErr
	}

	u := fmt.Sprintf("http://%s%s", r.addr, fmt.Sprintf(volumeGetPath, name))
	fmt.Println(u)

	resp, err := apiRequest(u, "GET", nil)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	contents, _ := ioutil.ReadAll(resp.Body)
	log.Printf("response body for get-volumes:\n%#v", string(contents))
	if err != nil {
		log.Printf("Volume list can not be fetched.")
		return "", "", "", err
	}

	var appInstance interface{}
	if err := json.Unmarshal([]byte(contents), &appInstance); err != nil {
		log.Printf("json decoder failed for response.")
		return "", "", "", err
	}

	storageInstance := appInstance.(map[string]interface{})
	storage := storageInstance["storage_instances"].(map[string]interface{})

	var iqn string
	var portal string
	var volUUID string

	for k1, v1 := range storage {
		fmt.Println("key1: ", k1)
		fmt.Println("Value1:\n", v1)

		storageInstance := v1.(map[string]interface{})

		access := storageInstance["access"].(map[string]interface{})
		log.Printf("access = ", access)

		storageIP := access["ips"].([]interface{})
		log.Printf("storageIP =", storageIP[0].(string))
		portal = storageIP[0].(string)

		storageIQN := access["iqn"].(string)
		log.Printf("storageIQN = ", storageIQN)
		iqn = storageIQN

		volumes := storageInstance["volumes"].(map[string]interface{})
		log.Printf("volumes = ", volumes)

		for vol_key, vol_val := range volumes {
			log.Println("vol_key: ", vol_key)
			log.Println("vol_val: ", vol_val)

			volumeData := vol_val.(map[string]interface{})

			volumeName := volumeData["name"].(string)
			log.Printf("volumeName = ", volumeName)

			volumeUUID := volumeData["uuid"].(string)
			log.Printf("volumeUUID = ", volumeUUID)
			volUUID = volumeUUID

			break
		}

		storage_name := storageInstance["name"].(string)
		fmt.Println("storage name = ", storage_name)
	}

	log.Println(
		fmt.Sprintf("iqn = [%#v], portal = [%#v], volume-uuid = [%#v]", iqn, portal, volUUID))
	return iqn, portal, volUUID, err
}

func (r Client) MountVolume(name string, destination string, fsType string) error {
	iqn, portal, volUUID, err := r.GetIQNandPortal(name)
	if err != nil {
		log.Println(
			fmt.Sprintf("Unable to find IQN and portal for %#v.", name))
		return err
	}

	diskPath := fmt.Sprintf("/dev/disk/by-uuid/%s", volUUID)
	diskAvailable := waitForDisk(diskPath, 1)

	if diskAvailable {
		log.Println("Disk [%#v] is already available.", diskPath)
		return nil
	}

	if out, err :=
		exec.Command("iscsiadm", "-m", "discovery", "-t", "sendtargets", "-p", portal+":3260").CombinedOutput(); err != nil {
		log.Println("Unable to discover targets at portal %#v. Error output [%#v]", portal, string(out))
		return err
	}

	if out, err :=
		exec.Command("iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--login").CombinedOutput(); err != nil {
		log.Println("Unable to login to target %#v at portal %#v. Error output [%#v]",
			iqn,
			portal,
			string(out))
		return err
	}

	// wait for disk to be available after target login

	diskAvailable = waitForDisk(diskPath, 10)
	if !diskAvailable {
		log.Println(
			fmt.Sprintf("Device [%#v] is not available in 10 seconds", diskPath))
		return err
	}

	mounted, err := isAlreadyMounted(destination)
	if mounted {
		log.Println("destination mount-point[%#v] is in use already", destination)
		return err
	}

	// Mount the disk now to the destination
	if err := os.MkdirAll(destination, 0750); err != nil {
		log.Println("failed to create destination directory [%#v]", destination)
		return err
	}

	err = doMount(diskPath, destination, fsType, nil)
	if err != nil {
		log.Println("Unable to mount iscsi volume [%#v] to directory [%#v].", diskPath, destination)
		return err
	}

	return nil
}

func doMount(sourceDisk string, destination string, fsType string, mountOptions []string) error {
	log.Println("Mounting volume %#v to %#v, file-system %#v options %v",
		sourceDisk,
		destination,
		fsType,
		mountOptions)

	exec.Command("fsck", "-a", sourceDisk).CombinedOutput()

	if out, err :=
		exec.Command("mount", "-t", fsType,
			"-o", strings.Join(mountOptions, ","), sourceDisk, destination).CombinedOutput(); err != nil {
		log.Println(
			fmt.Sprintf("mount failed for volume [%#v]. output [%#v], error [%#v]", sourceDisk, string(out), err))
		log.Println(
			fmt.Sprintf("Checking for disk formatting [%#v]", sourceDisk))

		if fsType == "ext4" {
			log.Println("ext4 block fsType [%#v]", fsType)
			_, err =
				exec.Command("mkfs."+fsType, "-E",
					"lazy_itable_init=0,lazy_journal_init=0", "-F", sourceDisk).CombinedOutput()
		} else {
			log.Println(
				fmt.Sprintf("fsType [%#v]", fsType))
			_, err =
				exec.Command("mkfs."+fsType, sourceDisk).CombinedOutput()
		}
		if err == nil {
			log.Println("Done with formatting, mounting again.")
			if _, err := exec.Command("mount", "-t", fsType,
				"-o", strings.Join(mountOptions, ","),
				sourceDisk, destination).CombinedOutput(); err != nil {
				log.Println("Error in mounting. Error = ", err)
				return err
			} else {
				log.Println(
					fmt.Sprintf("Mounted [%#v] successfully on [%#v]", sourceDisk, destination))
				return nil
			}
		} else {
			log.Println("mkfs failed. Error = ", err)
		}
		return err
	}
	log.Println(
		fmt.Sprintf("Mounted [%#v] successfully on [%#v]", sourceDisk, destination))
	return nil
}

func doUnmount(destination string) error {
	log.Println("Unmounting %#v", destination)

	if out, err := exec.Command("umount", destination).CombinedOutput(); err != nil {
		log.Println(
			fmt.Sprintf("doUnmount:: Unmounting failed for [%#v]. output [%#v]", destination, out))
		log.Println("doUnmount:: error = ", err)
		return err
	}

	log.Println("Unmount successful.")

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
		log.Println(
			fmt.Sprintf("UnmountVolume:: Unable to find IQN and portal for %#v.", name))
		return err
	}

	err = doUnmount(destination)
	if err != nil {
		log.Println(
			fmt.Sprintf("Unable to unmount %#v", destination))
		return err
	}

	if out, err :=
		exec.Command("iscsiadm", "-m", "node", "-p", portal+":3260", "-T", iqn, "--logout").CombinedOutput(); err != nil {
		log.Println(
			fmt.Sprintf("Unable to logout target %#v at portal %#v. Error output [%#v]",
				iqn,
				portal,
				string(out)))
		return err
	}

	log.Println("UnmountVolume: iscsi session logout successful.")

	return nil
}

func responseCheck(resp *http.Response) error {
	var p response
	if !p.Ok {
		return fmt.Errorf(p.Err)
	}

	return nil
}

func apiRequest(restUrl string, method string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, restUrl, bytes.NewBuffer(body))
	req.Header.Set("auth-token", authToken)
	req.Header.Set("Content-Type", "application/json")
	hdr := fmt.Sprintf("Docker-Volume-%#v", VERSION)
	req.Header.Set("Datera-Driver", hdr)
	log.Printf("apiRequest restUrl [%#v], method [%#v], body [%#v], header [%#v]",
		restUrl, method, string(body), req.Header)

	client := &http.Client{}
	resp, err := client.Do(req)
	log.Println("Response Status: ", resp.Status)
	log.Println("Response Headers: ", resp.Header)
	return resp, err
}
