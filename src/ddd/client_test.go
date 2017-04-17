package main_test

import (
	driver "ddd"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	ADDR     = "172.19.1.41"
	PORT     = "7717"
	APIVER   = "2.1"
	USERNAME = "admin"
	PASSWORD = "password"
	TENANT   = "/root"
	TIMEOUT  = "30s"
	TOKEN    = "test1234"
)

func TestClientCreate(t *testing.T) {
	assert := assert.New(t)
	client := driver.NewClient(ADDR, USERNAME, PASSWORD, TENANT, true, false, driver.VERSION, APIVER)

	assert.NotEmpty(client)
}

func TestClientCreateVolume(t *testing.T) {
	assert := assert.New(t)
	client := driver.NewClient(ADDR, USERNAME, PASSWORD, TENANT, true, false, driver.VERSION, APIVER)
	name := "test-create-volume"
	size := 5
	err := client.CreateVolume(name, size, 1, "", 0, 0, "hybrid")
	assert.NoError(err)
}

func TestClientDetachVolume(t *testing.T) {
	assert := assert.New(t)
	client := driver.NewClient(ADDR, USERNAME, PASSWORD, TENANT, true, false, driver.VERSION, APIVER)
	name := "test-create-detach-volume"
	size := 5
	err := client.CreateVolume(name, size, 1, "", 0, 0, "hybrid")
	assert.NoError(err)

	err = client.DetachVolume(name)
	assert.NoError(err)
}

func TestClientDeleteVolume(t *testing.T) {
	assert := assert.New(t)
	client := driver.NewClient(ADDR, USERNAME, PASSWORD, TENANT, true, false, driver.VERSION, APIVER)
	name := "test-create-delete-volume"
	size := 5
	err := client.CreateVolume(name, size, 1, "", 0, 0, "hybrid")
	assert.NoError(err)

	err = client.DeleteVolume(name, "/mnt/test")
	assert.NoError(err)
}

func TestClientMountUnmountVolume(t *testing.T) {
	assert := assert.New(t)
	client := driver.NewClient(ADDR, USERNAME, PASSWORD, TENANT, true, false, driver.VERSION, APIVER)
	name := "test-create-mount-volume"
	size := 5
	err := client.CreateVolume(name, size, 1, "", 0, 0, "hybrid")
	assert.NoError(err)

	client.MountVolume(name, "/mnt/tvol1", "ext4", "some-uuid")
	client.UnmountVolume(name, "/mnt/tvol1")
}

func TestForceClean(t *testing.T) {
	client := driver.NewClient(ADDR, USERNAME, PASSWORD, TENANT, true, false, driver.VERSION, APIVER)
	client.Api.ForceClean()
}
