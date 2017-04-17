package main_test

import (
	driver "ddd"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	dv "github.com/docker/go-plugins-helpers/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	testDefaultDir = "/mnt"
	testRESTAddr   = "http://testmachine.com:8888"
	testUsername   = "test-user"
	testPassword   = "12345678"
	testDebug      = true
	testNoSSL      = false
	testTenant     = "/root"
)

var (
	testMountRequest = dv.MountRequest{
		Name: "testRequest"}
	testUnmountRequest = dv.UnmountRequest{
		Name: "testRequest"}
	testRequest = dv.Request{
		Name: "testRequest",
		Options: map[string]string{
			"size":     "42",
			"replica":  "3",
			"template": "testtemp",
			"fstype":   "testfstype",
			"maxiops":  "0",
			"maxbw":    "0"}}
)

func TestDriverCreate(t *testing.T) {
	assert := assert.New(t)
	mockClient := new(MockDateraClient)
	mockClient.On("VolumeExist", testRequest.Name).Return(false, nil)
	mockClient.On("CreateVolume", testRequest.Name, 42, 3, "testtemp", 0, 0, "hybrid").Return(nil)
	d := driver.NewDateraDriver(testRESTAddr,
		testUsername,
		testPassword,
		testTenant,
		testDebug,
		testNoSSL)
	d.DateraClient = mockClient
	enddir := filepath.Join(testDefaultDir, testRequest.Name)

	result := d.Create(testRequest)
	mockClient.AssertExpectations(t)
	assert.Equal(dv.Response{}, result)
	vmap := d.Volumes
	assert.Equal(len(vmap), 1)
	assert.NotNil(vmap[enddir])
}

func TestDriverRemove(t *testing.T) {
	assert := assert.New(t)
	mockClient := new(MockDateraClient)
	mockClient.On("VolumeExist", testRequest.Name).Return(false, nil)
	mockClient.On("CreateVolume", testRequest.Name, 42, 3, "testtemp", 0, 0, "hybrid").Return(nil)
	mockClient.On("DeleteVolume", testRequest.Name, "/mnt/"+testRequest.Name).Return(nil)
	d := driver.NewDateraDriver(testRESTAddr,
		testUsername,
		testPassword,
		testTenant,
		testDebug,
		testNoSSL)
	d.DateraClient = mockClient
	enddir := filepath.Join(testDefaultDir, testRequest.Name)

	// Test deleting a volume that exists
	d.Create(testRequest)
	result := d.Remove(testRequest)
	assert.Equal(dv.Response{}, result)
	vmap := d.Volumes
	assert.Equal(len(vmap), 0)
	assert.Nil(vmap[enddir])

	// Test deleting a volume that doesn't exist
	mockClient.On("DeleteVolume", testRequest.Name, "/mnt/"+testRequest.Name).Return(nil)
	result = d.Remove(testRequest)
	assert.Equal(dv.Response{}, result)
	vmap = d.Volumes
	assert.Equal(len(vmap), 0)
	assert.Nil(vmap[enddir])
	mockClient.AssertExpectations(t)
}

func TestDriverList(t *testing.T) {
	assert := assert.New(t)
	mockClient := new(MockDateraClient)
	mockClient.On("CreateVolume", testRequest.Name, 42, 3, "testtemp", 0, 0, "hybrid").Return(nil)
	mockClient.On("VolumeExist", testRequest.Name).Return(false, nil)
	d := driver.NewDateraDriver(testRESTAddr,
		testUsername,
		testPassword,
		testTenant,
		testDebug,
		testNoSSL)
	d.DateraClient = mockClient
	result := d.List(testRequest)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)

	assert.Equal(dv.Response{Volumes: nil}, result)

	d.Create(testRequest)
	vols := []*dv.Volume{}
	vols = append(vols, &dv.Volume{Name: testRequest.Name, Mountpoint: enddir})
	result = d.List(testRequest)
	assert.Equal(dv.Response{Volumes: vols}, result)
	mockClient.AssertExpectations(t)
}

func TestDriverGet(t *testing.T) {
	assert := assert.New(t)
	mockClient := new(MockDateraClient)
	mockClient.On("CreateVolume", testRequest.Name, 42, 3, "testtemp", 0, 0, "hybrid").Return(nil)
	mockClient.On("VolumeExist", testRequest.Name).Return(false, nil)
	d := driver.NewDateraDriver(testRESTAddr,
		testUsername,
		testPassword,
		testTenant,
		testDebug,
		testNoSSL)
	d.DateraClient = mockClient
	result := d.Get(testRequest)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)
	assert.Equal(dv.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", enddir)}, result)

	d.Create(testRequest)
	result = d.Get(testRequest)
	st := make(map[string]interface{})
	assert.Equal(dv.Response{Volume: &dv.Volume{Name: testRequest.Name, Mountpoint: enddir, Status: st}}, result)
	mockClient.AssertExpectations(t)
}

func TestDriverPath(t *testing.T) {
	assert := assert.New(t)
	mockClient := new(MockDateraClient)
	d := driver.NewDateraDriver(testRESTAddr,
		testUsername,
		testPassword,
		testTenant,
		testDebug,
		testNoSSL)
	d.DateraClient = mockClient
	result := d.Path(testRequest)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)
	assert.Equal(dv.Response{Mountpoint: enddir}, result)
	mockClient.AssertExpectations(t)
}

func TestDriverMount(t *testing.T) {
	assert := assert.New(t)
	mockClient := new(MockDateraClient)
	mockClient.On("CreateVolume", testRequest.Name, 42, 3, "testtemp", 0, 0, "hybrid").Return(nil)
	mockClient.On("VolumeExist", testRequest.Name).Return(false, nil)
	mockClient.On("MountVolume", testRequest.Name, "/mnt/"+testRequest.Name, "ext4", "some-volid").Return(nil)
	mockClient.On("LoginVolume", testRequest.Name, "/mnt/"+testRequest.Name).Return("some-volid", nil)
	d := driver.NewDateraDriver(testRESTAddr,
		testUsername,
		testPassword,
		testTenant,
		testDebug,
		testNoSSL)
	d.DateraClient = mockClient
	d.Create(testRequest)
	result := d.Mount(testMountRequest)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)
	assert.Equal(dv.Response{Mountpoint: enddir}, result)
	assert.Equal(len(d.Volumes), 1)
	mockClient.AssertExpectations(t)
}

func TestDriverUnmount(t *testing.T) {
	assert := assert.New(t)
	mockClient := new(MockDateraClient)
	mockClient.On("CreateVolume", testRequest.Name, 42, 3, "testtemp", 0, 0, "hybrid").Return(nil)
	mockClient.On("VolumeExist", testRequest.Name).Return(false, nil)
	mockClient.On("UnmountVolume", testRequest.Name, "/mnt/"+testRequest.Name).Return(nil)
	d := driver.NewDateraDriver(testRESTAddr,
		testUsername,
		testPassword,
		testTenant,
		testDebug,
		testNoSSL)
	d.DateraClient = mockClient
	d.Create(testRequest)
	d.Volumes[d.MountPoint(testRequest.Name)].Connections = 1
	result := d.Unmount(testUnmountRequest)
	assert.Equal(dv.Response{}, result)
	assert.Equal(len(d.Volumes), 1)
	mockClient.AssertExpectations(t)
}

///////////////////////////////////////////////////////
// Mock the DateraClient object for testing the driver
///////////////////////////////////////////////////////
type MockDateraClient struct {
	mock.Mock
	addr     string
	username string
	password string
}

func (m *MockDateraClient) VolumeExist(name string) (bool, error) {
	args := m.Called(name)
	return args.Bool(0), args.Error(1)
}
func (m *MockDateraClient) CreateVolume(name string, size int, replica int, template string, maxIops int, maxBW int, placementMode string) error {
	args := m.Called(name, size, replica, template, maxIops, maxBW, placementMode)
	return args.Error(0)
}
func (m *MockDateraClient) DeleteVolume(name string, mountpoint string) error {
	args := m.Called(name, mountpoint)
	return args.Error(0)
}
func (m *MockDateraClient) LoginVolume(name string, destination string) (string, error) {
	args := m.Called(name, destination)
	return args.String(0), args.Error(1)
}
func (m *MockDateraClient) MountVolume(name string, destination string, fsType string, volUUID string) error {
	args := m.Called(name, destination, fsType, volUUID)
	return args.Error(0)
}
func (m *MockDateraClient) UnmountVolume(name string, destination string) error {
	args := m.Called(name, destination)
	return args.Error(0)
}
func (m *MockDateraClient) DetachVolume(name string) error {
	args := m.Called(name)
	return args.Error(0)
}
func (m *MockDateraClient) GetIQNandPortal(name string) (string, string, string, error) {
	args := m.Called(name)
	return "", "", "", args.Error(0)
}
func (m *MockDateraClient) FindDeviceFsType(name string) (string, error) {
	args := m.Called(name)
	return "", args.Error(0)
}

///////////////////////////////////////////////////////

//////////////////////////
// Mock FileInfo object //
//////////////////////////
type MFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modtime time.Time
	isdir   bool
	sys     interface{}
}

func (m MFileInfo) Name() string {
	return m.name
}

func (m MFileInfo) Size() int64 {
	return m.size
}

func (m MFileInfo) Mode() os.FileMode {
	return m.mode
}

func (m MFileInfo) ModTime() time.Time {
	return m.modtime
}

func (m MFileInfo) IsDir() bool {
	return true
}

func (m MFileInfo) Sys() interface{} {
	return m.sys
}

///////////////////////////
// Mock System/OS object //
///////////////////////////
type MSystem struct {
}

func (s MSystem) Lstat(f string) (os.FileInfo, error) {
	return MFileInfo{}, nil
}

func (s MSystem) Stat(f string) (os.FileInfo, error) {
	return MFileInfo{}, nil
}

func (s MSystem) IsNotExist(e error) bool {
	return false
}

func (s MSystem) MkdirAll(f string, o os.FileMode) error {
	return nil
}

func TestMain(m *testing.M) {
	driver.OS = MSystem{}
	driver.FileReader = func(s string) ([]byte, error) {
		return []byte("InitiatorName=iqn.1993-08.org.debian:01:71be38c985a"), nil
	}
	driver.IsAlreadyMounted = func(destination string) (bool, error) { return true, nil }
	os.Exit(m.Run())
}
