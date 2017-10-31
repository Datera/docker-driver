package common

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	LoginUrl    = "http://%s:7717/v2.2/login"
	LogUrl      = "http://%s:7717/v2.2/logs_upload"
	LogInterval = int(time.Hour * 1)
	LastFile    = "/var/log/datera/last"
	LogFiltered = "/var/log/datera/dlogs.tar.gz"
)

var (
	EndLogging = make(chan bool)
)

func putRequest(url string, data io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, url, data)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func logUpload(ip, username, password, file string, whole bool) error {
	// Login and get API key
	params := new(bytes.Buffer)
	json.NewEncoder(params).Encode(map[string]string{"name": username, "password": password})
	url := fmt.Sprintf(LoginUrl, ip)
	res, err := putRequest(url, params)
	if err != nil {
		return err
	}
	data := make(map[string]interface{}, 5)
	err = json.NewDecoder(res.Body).Decode(&data)
	if err != nil {
		return err
	}
	key := data["key"]

	url = fmt.Sprintf(LogUrl, ip)

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	err = w.WriteField("ecosystem", "docker")
	if err != nil {
		return err
	}

	fw, err := w.CreateFormFile("logs.tar.gz", file)
	if err != nil {
		return err
	}
	if _, err = io.Copy(fw, f); err != nil {
		return err
	}
	// Don't forget to close the multipart writer.
	// If you don't close it, your request will be missing the terminating boundary.
	w.Close()

	// Now that you have a form, you can submit it to your handler.
	req, err := http.NewRequest(http.MethodPut, url, &b)
	if err != nil {
		return err
	}
	// Don't forget to set the content type, this will contain the boundary.
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Auth-Token", key.(string))

	// Submit the request
	client := &http.Client{}
	res, err = client.Do(req)
	log.Debugf("Status Code: %s", res.StatusCode)
	if err != nil {
		return err
	}

	// Check the response
	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("bad status: %s", res.Status)
		bodyBytes, _ := ioutil.ReadAll(res.Body)
		log.Error(err)
		log.Error(string(bodyBytes))
		return err
	}
	return nil
}

func getLastTime() (int64, error) {
	var it int64
	if _, err := os.Stat(LastFile); os.IsNotExist(err) {
		it = 0
	} else {
		t, err := ioutil.ReadFile(LastFile)
		if err != nil {
			return 0, err
		}
		it, err = strconv.ParseInt(string(t), 10, 64)
		if err != nil {
			return 0, err
		}
	}
	t := []byte(strconv.Itoa(int(time.Now().Unix())))
	if err := ioutil.WriteFile(LastFile, t, 0644); err != nil {
		return 0, err
	}
	return int64(it), nil
}

func filterCompressLogsByTime(file, newFile string, last int64) error {
	// Open src and destination files
	srcf, err := os.Open(file)
	if err != nil {
		return err
	}
	fstat, err := srcf.Stat()
	if err != nil {
		return err
	}
	dstf, err := os.Create(newFile)
	if err != nil {
		return err
	}
	// Get archive handles and reader
	gw := gzip.NewWriter(dstf)
	tw := tar.NewWriter(gw)
	read := bufio.NewScanner(srcf)

	// Make sure all our resources clean up
	defer gw.Close()
	defer tw.Close()
	defer srcf.Close()
	defer dstf.Close()

	// Write archive header
	hdr := &tar.Header{
		Name: file,
		Mode: 0600,
		Size: fstat.Size(),
	}
	if err = tw.WriteHeader(hdr); err != nil {
		return err
	}

	// Write Archive
	var entry map[string]interface{}
	for read.Scan() {
		// We only care about json lines
		if err = json.Unmarshal([]byte(read.Text()), &entry); err == nil {
			// Only write lines that have a timestamp greater than the one
			// passed in to the function
			if t, err := time.Parse(time.RFC3339, entry["time"].(string)); err == nil && t.Unix() >= last {
				if _, err = tw.Write([]byte(read.Text())); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// For finer grained sleeping, interval is specified in seconds
func logSleeper(interval int) {
	for {
		if interval <= 0 {
			break
		}
		time.Sleep(time.Second * 1)
		interval--
	}
}

func LogUploadDaemon(ip, username, password, file string, interval int) error {
	for {
		log.Debug("Getting last time logs were collected")
		time, err := getLastTime()
		if err != nil {
			return err
		}
		log.Debugf("Filtering logs by time, %d\n", time)
		if err := filterCompressLogsByTime(file, LogFiltered, time); err != nil {
			return err
		}

		// Determine if filtered logs exist
		lf, err := os.Open(LogFiltered)
		if err != nil {
			return err
		}
		defer lf.Close()
		fstat, err := lf.Stat()
		if err != nil {
			return err
		}
		// Even a single line of logs will be greater than 100 bytes
		if fstat.Size() > 100 {
			log.Debug("Uploading logs")
			if err = logUpload(ip, username, password, LogFiltered, false); err != nil {
				return err
			}
		} else {
			log.Debugf("No new filtered logs detected.  Size: %d\n", fstat.Size())
		}
		log.Debugf("Logpush sleeping %d", interval)
		logSleeper(interval)
	}
}
