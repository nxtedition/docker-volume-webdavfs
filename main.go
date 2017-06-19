package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

const socketAddress = "/run/docker/plugins/davfs.sock"

type davfsVolume struct {
	URL      string
	Conf     string
	UID      uint64
	GID      uint64
	FileMode string
	DirMode  string
	Ro       bool
	Rw       bool
	Exec     bool
	Suid     bool
	Grpid    bool
	Netdev   bool

	Mountpoint  string
	connections int
}

type davfsDriver struct {
	sync.RWMutex

	root      string
	statePath string
	volumes   map[string]*davfsVolume
}

func newdavfsDriver(root string) (*davfsDriver, error) {
	logrus.WithField("method", "new driver").Debug(root)

	d := &davfsDriver{
		root:      filepath.Join(root, "volumes"),
		statePath: filepath.Join(root, "state", "davfs-state.json"),
		volumes:   map[string]*davfsVolume{},
	}

	data, err := ioutil.ReadFile(d.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.WithField("statePath", d.statePath).Debug("no state found")
		} else {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, &d.volumes); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func (d *davfsDriver) saveState() {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		logrus.WithField("statePath", d.statePath).Error(err)
		return
	}

	if err := ioutil.WriteFile(d.statePath, data, 0644); err != nil {
		logrus.WithField("savestate", d.statePath).Error(err)
	}
}

func (d *davfsDriver) Create(r volume.Request) volume.Response {
	logrus.WithField("method", "create").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v := &davfsVolume{}

	for key, val := range r.Options {
		switch key {
		case "url":
			v.URL = val
		case "conf":
			v.Conf = val
		case "uid":
			u, err := strconv.ParseUint(val, 10, 64)
			if err != nil {
				return responseError("'uid' option must be int")
			}
			v.UID = u
		case "gid":
			u, err := strconv.ParseUint(val, 10, 64)
			if err != nil {
				return responseError("'uid' option must be int")
			}
			v.GID = u
		case "file_mode":
			v.FileMode = val
		case "dir_mode":
			v.DirMode = val
		case "ro":
			v.Ro = true
		case "rw":
			v.Rw = true
		case "exec":
			v.Exec = true
		case "suid":
			v.Suid = true
		case "grpid":
			v.Grpid = true
		case "_netdav":
			v.Netdev = true
		default:
			return responseError(fmt.Sprintf("unknown option %q", val))
		}
	}

	if v.URL == "" {
		return responseError("'url' option required")
	}
	_, err := url.Parse(v.URL)
	if err != nil {
		return responseError("'url' option malformed")
	}
	v.Mountpoint = filepath.Join(d.root, fmt.Sprintf("%x", md5.Sum([]byte(v.URL))))

	d.volumes[r.Name] = v

	d.saveState()

	return volume.Response{}
}

func (d *davfsDriver) Remove(r volume.Request) volume.Response {
	logrus.WithField("method", "remove").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	if v.connections != 0 {
		return responseError(fmt.Sprintf("volume %s is currently used by a container", r.Name))
	}
	if err := os.RemoveAll(v.Mountpoint); err != nil {
		return responseError(err.Error())
	}
	delete(d.volumes, r.Name)
	d.saveState()
	return volume.Response{}
}

func (d *davfsDriver) Path(r volume.Request) volume.Response {
	logrus.WithField("method", "path").Debugf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	return volume.Response{Mountpoint: v.Mountpoint}
}

func (d *davfsDriver) Mount(r volume.MountRequest) volume.Response {
	logrus.WithField("method", "mount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	if v.connections == 0 {
		fi, err := os.Lstat(v.Mountpoint)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(v.Mountpoint, 0755); err != nil {
				return responseError(err.Error())
			}
		} else if err != nil {
			return responseError(err.Error())
		}

		if fi != nil && !fi.IsDir() {
			return responseError(fmt.Sprintf("%v already exist and it's not a directory", v.Mountpoint))
		}

		if err := d.mountVolume(v); err != nil {
			return responseError(err.Error())
		}
	}

	v.connections++

	return volume.Response{Mountpoint: v.Mountpoint}
}

func (d *davfsDriver) Unmount(r volume.UnmountRequest) volume.Response {
	logrus.WithField("method", "unmount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	v.connections--

	if v.connections <= 0 {
		if err := d.unmountVolume(v.Mountpoint); err != nil {
			return responseError(err.Error())
		}
		v.connections = 0
	}

	return volume.Response{}
}

func (d *davfsDriver) Get(r volume.Request) volume.Response {
	logrus.WithField("method", "get").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	return volume.Response{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.Mountpoint}}
}

func (d *davfsDriver) List(r volume.Request) volume.Response {
	logrus.WithField("method", "list").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.Mountpoint})
	}
	return volume.Response{Volumes: vols}
}

func (d *davfsDriver) Capabilities(r volume.Request) volume.Response {
	logrus.WithField("method", "capabilities").Debugf("%#v", r)

	return volume.Response{Capabilities: volume.Capability{Scope: "local"}}
}

func (d *davfsDriver) mountVolume(v *davfsVolume) error {
	u, err := url.Parse(v.URL)
	if err != nil {
		log.Fatal(err)
	}

	cmd := exec.Command("mount.davfs", fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path), v.Mountpoint)

	if v.Conf != "" {
		cmd.Args = append(cmd.Args, "-o", fmt.Sprintf("conf=%s", v.Conf))
	}
	if v.UID != 0 {
		cmd.Args = append(cmd.Args, "-o", fmt.Sprintf("uid=%d", v.UID))
	}
	if v.GID != 0 {
		cmd.Args = append(cmd.Args, "-o", fmt.Sprintf("gid=%d", v.GID))
	}
	if v.FileMode != "" {
		cmd.Args = append(cmd.Args, "-o", fmt.Sprintf("file_mode=%s", v.FileMode))
	}
	if v.DirMode != "" {
		cmd.Args = append(cmd.Args, "-o", fmt.Sprintf("dir_mode=%s", v.DirMode))
	}
	if v.Ro {
		cmd.Args = append(cmd.Args, "-o", "ro")
	}
	if v.Rw {
		cmd.Args = append(cmd.Args, "-o", "rw")
	}
	if v.Exec {
		cmd.Args = append(cmd.Args, "-o", "exec")
	}
	if v.Suid {
		cmd.Args = append(cmd.Args, "-o", "suid")
	}
	if v.Grpid {
		cmd.Args = append(cmd.Args, "-o", "grpid")
	}
	if v.Netdev {
		cmd.Args = append(cmd.Args, "-o", "_netdev")
	}

	username := u.User.Username()
	if username != "" {
		password, _ := u.User.Password()
		cmd.Stdin = strings.NewReader(fmt.Sprintf("%s\n%s", username, password))
	}

	logrus.Debug(cmd.Args)
	return cmd.Run()
}

func (d *davfsDriver) unmountVolume(target string) error {
	cmd := fmt.Sprintf("umount %s", target)
	logrus.Debug(cmd)
	return exec.Command("sh", "-c", cmd).Run()
}

func responseError(err string) volume.Response {
	logrus.Error(err)
	return volume.Response{Err: err}
}

func main() {
	debug := os.Getenv("DEBUG")
	if ok, _ := strconv.ParseBool(debug); ok {
		logrus.SetLevel(logrus.DebugLevel)
	}
	logrus.SetLevel(logrus.DebugLevel)

	// make sure "/etc/davfs2/secrets" is owned by root
	err := os.Chown("/etc/davfs2/secrets", 0, 0)
	if err != nil {
		log.Fatal(err)
	}

	d, err := newdavfsDriver("/mnt")
	if err != nil {
		log.Fatal(err)
	}
	h := volume.NewHandler(d)
	logrus.Infof("listening on %s", socketAddress)
	logrus.Error(h.ServeUnix(socketAddress, 0))
}
