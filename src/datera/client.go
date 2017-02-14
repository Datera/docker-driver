package datera

import (
	"bytes"
	"crypto/tls"
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
	volumesPath      = "/v2.1/app_instances"
	volumeCreatePath = "/v2.1/app_instances"
	volumeStopPath   = "/v2.1/app_instances/%s"
	volumeGetPath    = "/v2.1/app_instances/%s"
	initiatorPath    = "/v2.1/initiators"
	aclPath          = "/v2.1/app_instances/%s/storage_instances/%s/acl_policy"
	loginPath        = "/v2.1/login"
	VERSION          = "2.0.0"
	initiatorFile    = "/etc/iscsi/initiatorname.iscsi"
)

var (
	authToken string
)

type peer struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type appsresponse struct {
	Tenant  string        `json:"tenant"`
	Path    string        `json:"path"`
	Version string        `json:"version"`
	Data    []appinstance `json:"data"`
}

type appresponse struct {
	Tenant  string      `json:"tenant"`
	Path    string      `json:"path"`
	Version string      `json:"version"`
	Data    appinstance `json:"data"`
}

type appinstance struct {
	Tenant           string            `json:"tenant"`
	Path             string            `json:"path"`
	Name             string            `json:"name"`
	Id               string            `json:"id"`
	Health           string            `json:"health"`
	Description      string            `json:"description"`
	AdminState       string            `json:"admin_state"`
	StorageInstances []storageinstance `json:"storage_instances"`
	AppTemplate      apptemplate       `json:"app_template"`
}

type storageinstance struct {
	Health            string                 `json:"health"`
	Path              string                 `json:"path"`
	Name              string                 `json:"name"`
	AdminState        string                 `json:"admin_state"`
	OpState           string                 `json:"op_state"`
	Volumes           []volume               `json:"volumes"`
	AccessControlMode string                 `json:"access_control_mode"`
	AclPolicy         map[string]interface{} `json:"acl_policy"`
	IpPool            map[string]interface{} `json:"ip_pool"`
	Access            access                 `json:"access"`
}

type apptemplate struct {
	Path           string `json:"path"`
	ResolvedPath   string `json:"resolved_path"`
	ResolvedTenant string `json:"resolved_tenant"`
}

type access struct {
	Path string   `json:"path"`
	Ips  []string `json:"ips"`
	Iqn  string   `json:"iqn,omitempty"`
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
	driver   string
	version  string
	schema   string
	tenant   string
}

func DebugClient(addr string) *Client {
	myAddr := fmt.Sprintf("%s:7718", addr)
	client := NewClient(myAddr, "root", "admin", "password", "root", true, true, "Dockers-Debug", VERSION)
	return client
}

func NewClient(addr, base, username, password, tenant string, debug, ssl bool, driver, version string) *Client {
	var schema string
	if ssl {
		schema = "https"
	} else {
		schema = "http"
	}
	client := &Client{addr, base, username, password, debug, driver, version, schema, tenant}
	log.Printf("Client: %#v", client)
	return client
}

func (r Client) Login() error {
	log.Printf("Login to [%#v] with user [%#v]", r.addr, r.username)
	url := fmt.Sprintf("%s://%s%s", r.schema, r.addr, fmt.Sprintf(loginPath))
	fmt.Println(url)

	var jsonStr = []byte(
		fmt.Sprintf(`
			    {
				"name": "%s",
				"password": "%s"
			    }`, r.username, r.password))
	authToken = ""
	resp, err := r.apiRequest(url, "PUT", jsonStr, "")
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
	authErr := r.Login()
	if authErr != nil {
		log.Println("Authentication Failure.")
		return nil, authErr
	}
	u := fmt.Sprintf("%s://%s%s", r.schema, r.addr, volumesPath)

	res, err := r.apiRequest(u, "GET", nil, "")
	defer res.Body.Close()

	contents, _ := ioutil.ReadAll(res.Body)
	log.Println("response body for get-volumes:\n", string(contents))
	if err != nil {
		log.Printf("Volume list can not be fetched.")
		return nil, err
	}

	var app appsresponse
	if err := json.Unmarshal([]byte(contents), &app); err != nil {
		log.Printf("json decoder failed for response.")
		return nil, err
	}

	var outVolumes []volume

	for _, ai := range app.Data {
		outVolumes = append(outVolumes, ai.StorageInstances[0].Volumes[0])
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

	authErr := r.Login()
	if authErr != nil {
		log.Println("Authentication Failure.")
		return authErr
	}

	log.Printf("template [%#v], maxIops %d, maxBW %d", template, maxIops, maxBW)
	templateUsed := false
	if len(template) != 0 {
		templateUsed = true
	}
	u := fmt.Sprintf("%s://%s%s", r.schema, r.addr, fmt.Sprintf(volumeCreatePath))
	fmt.Println(u)

	var jsonStr string
	if templateUsed == false {
		jsonStr =
			`{"name":"` + name + `",
			  "access_control_mode":"deny_all",
			  "storage_instances": [ {
						"name":"storage-1",
						"volumes":[ {
							"name":"` + name + `",
							"replica_count":` + strconv.Itoa(int(replica)) + `,
							"size":` + strconv.Itoa(int(size)) + `,
							"snapshot_policies":[] }]
						}]
				}`
	} else {
		jsonStr =
			`{"name":"` + name + `",
			  "access_control_mode":"deny_all",
			  "app_template":"/app_templates/` + template + `"
		}`
	}

	log.Println("jsonStr:\n", jsonStr)
	resp, err := r.apiRequest(u, "POST", []byte(jsonStr), "")
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return err
	}
	body, _ := ioutil.ReadAll(resp.Body)
	log.Println("Response Body:\n", string(body))
	fmt.Println("response Body:", string(body))

	r.CreateACL(name)

	return responseCheck(resp)
}

func (r Client) CreateACL(volname string) error {
	authErr := r.Login()
	if authErr != nil {
		log.Println("Authentication Failure.")
		return authErr
	}
	// Parse InitiatorName
	dat, err := ioutil.ReadFile(initiatorFile)
	if err != nil {
		log.Printf("Could not read file %#v", initiatorFile)
	}
	initiator := strings.Split(strings.TrimSpace(string(dat)), "=")[1]
	log.Printf(initiator)

	// Create the initiator
	jsonStr := `{"name": "` + volname + `", "id": "` + initiator + `"}`
	initiators_url := fmt.Sprintf("%s://%s%s", r.schema, r.addr, initiatorPath)
	resp, err := r.apiRequest(initiators_url, "POST", []byte(jsonStr), "ConflictError")
	if err != nil {
		log.Printf("Initiator Creation Response Error: %s", err)
		return err
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	body, _ := ioutil.ReadAll(resp.Body)
	log.Println("response Body:\n", string(body))

	// Get the relevant app_instance
	appUrl := fmt.Sprintf("%s://%s%s", r.schema, r.addr, fmt.Sprintf(volumeGetPath, volname))
	resp, err = r.apiRequest(appUrl, "GET", nil, "")
	body, _ = ioutil.ReadAll(resp.Body)
	log.Println("response Body:\n", string(body))

	// Parse out storage_instance
	var appresp appresponse
	if err := json.Unmarshal([]byte(body), &appresp); err != nil {
		log.Println("json decoder failed for response: ", body)
		return err
	}

	log.Println("App Instance Body: ", appresp)
	for _, si := range appresp.Data.StorageInstances {
		log.Println("Storage Instance: ", si.Name)
		aclUrl := fmt.Sprintf("%s://%s%s", r.schema, r.addr,
			fmt.Sprintf(aclPath, volname, si.Name))
		jsonStr := fmt.Sprintf(`{"initiators": [{"path": "/initiators/%s"}]}`, initiator)
		r.apiRequest(aclUrl, "PUT", []byte(jsonStr), "")
	}

	return nil
}

func (r Client) DetachVolume(name string) error {
	log.Println("DetachVolume invoked for ", name)
	u := fmt.Sprintf("%s://%s%s", r.schema, r.addr, fmt.Sprintf(volumeStopPath, name))

	var jsonStr string
	jsonStr =
		`{"admin_state": "offline",
	"force": true
}`
	resp, err := r.apiRequest(u, "PUT", []byte(jsonStr), "")
	if resp != nil {
		defer resp.Body.Close()
	}
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
	authErr := r.Login()
	if authErr != nil {
		fmt.Println("Authentication Failure.")
		return authErr
	}

	err := r.DetachVolume(name)
	u := fmt.Sprintf("%s://%s%s", r.schema, r.addr, fmt.Sprintf(volumeStopPath, name))

	_, err = r.apiRequest(u, "DELETE", nil, "")
	if err != nil {
		log.Println("Error in delete operation.")
		return err
	}

	//return responseCheck(resp)
	return nil
}

func (r Client) GetIQNandPortal(name string) (string, string, string, error) {
	log.Printf("GetIQNandPortal invoked for [%#v]", name)
	authErr := r.Login()
	if authErr != nil {
		fmt.Println("Authentication Failure.")
		return "", "", "", authErr
	}

	u := fmt.Sprintf("%s://%s%s", r.schema, r.addr, fmt.Sprintf(volumeGetPath, name))
	fmt.Println(u)

	resp, err := r.apiRequest(u, "GET", nil, "")
	if err != nil {
		return "", "", "", err
	}
	if resp != nil {
		defer resp.Body.Close()
	}
	contents, _ := ioutil.ReadAll(resp.Body)
	log.Printf("response body for get-volumes:\n%s", string(contents))
	if err != nil {
		log.Printf("Volume list can not be fetched.")
		return "", "", "", err
	}

	var appresp appresponse
	if err := json.Unmarshal([]byte(contents), &appresp); err != nil {
		log.Printf("json decoder failed for response.")
		return "", "", "", err
	}

	var iqn string
	var portal string
	var volUUID string

	for _, siData := range appresp.Data.StorageInstances {

		iqn = siData.Access.Iqn
		portal = siData.Access.Ips[0]
		volUUID = siData.Volumes[0].Uuid
		break
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
					"lazy_itable_init=0,lazy_journal_init=0,nodiscard", "-F", sourceDisk).CombinedOutput()
		} else if fsType == "xfs" {
			log.Println(
				fmt.Sprintf("fsType [%#v]", fsType))
			_, err =
				exec.Command("mkfs."+fsType, "-K", sourceDisk).CombinedOutput()
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
			fmt.Sprintf("doUnmount:: Unmounting failed for [%#v]. output [%s]", destination, out))
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

// okFailMatchString will be used to substring search the response body
// for a string.  If that string is found, even if the response failed it will
// be treated as a success
func (r Client) apiRequest(restUrl string, method string, body []byte, okFailMatchString string) (*http.Response, error) {
	req, err := http.NewRequest(method, restUrl, bytes.NewBuffer(body))
	if err != nil {
		log.Println("Error Creating Request: ", err)
	}
	req.Header.Set("auth-token", authToken)
	req.Header.Set("Content-Type", "application/json")
	if !strings.HasSuffix(restUrl, "login") {
		req.Header.Set("tenant", "/"+r.tenant)
	}
	hdr := fmt.Sprintf("%s-%s", r.driver, r.version)
	req.Header.Set("Datera-Driver", hdr)
	log.Printf("apiRequest \nrestUrl [%#v],\nmethod [%#v],\n body: %s,\n header [%#v]",
		restUrl, method, string(body), req.Header)

	var client *http.Client
	if r.schema == "https" {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		client = &http.Client{Transport: tr}
	} else {
		client = &http.Client{}
	}
	resp, err := client.Do(req)
	if resp != nil {
		log.Println("Response Status: ", resp.Status)
		log.Println("Response Headers: ", resp.Header)
		// The body can only be read once and we need to read it here
		// So we'll wrap the contents in another reader with a no-op closer to
		// make sure it can be read again
		body, _ = ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		resp.Body = ioutil.NopCloser(bytes.NewReader(body))
		if okFailMatchString != "" && strings.Contains(string(body), okFailMatchString) {
			log.Println("Found Match: ", okFailMatchString, " in body: ", string(body))
			return resp, nil
		}
	} else {
		log.Println("Nil Response:", err)
	}
	return resp, err
}
