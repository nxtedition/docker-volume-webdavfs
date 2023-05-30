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

const socketAddress = "/run/docker/plugins/webdavfs.sock"

type webdavfsVolume struct {
	URL      string
	Username string
	Password string
	Conf     string
	UID      string
	GID      string
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

type webdavfsDriver struct {
	sync.RWMutex

	root      string
	statePath string
	volumes   map[string]*webdavfsVolume
}

func newwebdavfsDriver(root string) (*webdavfsDriver, error) {
	logrus.WithField("method", "new driver").Debug(root)

	d := &webdavfsDriver{
		root:      filepath.Join(root, "volumes"),
		statePath: filepath.Join(root, "state", "webdavfs-state.json"),
		volumes:   map[string]*webdavfsVolume{},
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

func (d *webdavfsDriver) saveState() {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		logrus.WithField("statePath", d.statePath).Error(err)
		return
	}

	if err := ioutil.WriteFile(d.statePath, data, 0644); err != nil {
		logrus.WithField("savestate", d.statePath).Error(err)
	}
}

func (d *webdavfsDriver) Create(r *volume.CreateRequest) error {
	logrus.WithField("method", "create").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v := &webdavfsVolume{}

	for key, val := range r.Options {
		switch key {
		case "url":
			v.URL = val
		case "username":
			v.Username = val
		case "password":
			v.Password = val
		case "conf":
			v.Conf = val
		case "uid":
			v.UID = val
		case "gid":
			v.GID = val
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
			return logError("unknown option %q", val)
		}
	}

	if v.URL == "" {
		return logError("'url' option required")
	}
	_, err := url.Parse(v.URL)
	if err != nil {
		return logError("'url' option malformed")
	}
	v.Mountpoint = filepath.Join(d.root, fmt.Sprintf("%x", md5.Sum([]byte(v.URL))))

	d.volumes[r.Name] = v
	d.saveState()

	return nil
}

func (d *webdavfsDriver) Remove(r *volume.RemoveRequest) error {
	logrus.WithField("method", "remove").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return logError("volume %s not found", r.Name)
	}

	if v.connections != 0 {
		return logError("volume %s is currently used by a container", r.Name)
	}
	if err := os.RemoveAll(v.Mountpoint); err != nil {
		return logError(err.Error())
	}
	delete(d.volumes, r.Name)
	d.saveState()
	return nil
}

func (d *webdavfsDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	logrus.WithField("method", "path").Debugf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.PathResponse{}, logError("volume %s not found", r.Name)
	}

	return &volume.PathResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *webdavfsDriver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	logrus.WithField("method", "mount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.MountResponse{}, logError("volume %s not found", r.Name)
	}

	if v.connections == 0 {
		fi, err := os.Lstat(v.Mountpoint)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(v.Mountpoint, 0755); err != nil {
				return &volume.MountResponse{}, logError(err.Error())
			}
		} else if err != nil {
			return &volume.MountResponse{}, logError(err.Error())
		}

		if fi != nil && !fi.IsDir() {
			return &volume.MountResponse{}, logError("%v already exist and it's not a directory", v.Mountpoint)
		}

		if err := d.mountVolume(v); err != nil {
			return &volume.MountResponse{}, logError(err.Error())
		}
	}
	v.connections++

	return &volume.MountResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *webdavfsDriver) Unmount(r *volume.UnmountRequest) error {
	logrus.WithField("method", "unmount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v, ok := d.volumes[r.Name]
	if !ok {
		return logError("volume %s not found", r.Name)
	}

	v.connections--

	if v.connections <= 0 {
		if err := d.unmountVolume(v.Mountpoint); err != nil {
			return logError(err.Error())
		}
		v.connections = 0
	}

	return nil
}

func (d *webdavfsDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	logrus.WithField("method", "get").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.GetResponse{}, logError("volume %s not found", r.Name)
	}

	return &volume.GetResponse{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.Mountpoint}}, nil
}

func (d *webdavfsDriver) List() (*volume.ListResponse, error) {
	logrus.WithField("method", "list").Debugf("")

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.Mountpoint})
	}
	return &volume.ListResponse{Volumes: vols}, nil
}

func (d *webdavfsDriver) Capabilities() *volume.CapabilitiesResponse {
	logrus.WithField("method", "capabilities").Debugf("")

	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: "local"}}
}

func (d *webdavfsDriver) mountVolume(v *webdavfsVolume) error {
	logrus.WithField("method", "mountVolume").Debugf("%#v", v)

	u, err := url.Parse(v.URL)
	if err != nil {
		log.Fatal(err)
	}
	logrus.WithField("method", "mountVolume").WithField("variable", "url").Debugf("%#v", u)

	cmd := exec.Command("mount.webdavfs", fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path), v.Mountpoint)

	if v.Conf != "" {
		cmd.Args = append(cmd.Args, "-o", fmt.Sprintf("conf=%s", v.Conf))
	}
	if v.UID != "" {
		exec.Command("adduser", "-S", "-u", v.UID, v.UID).Run()
		cmd.Args = append(cmd.Args, "-o", fmt.Sprintf("uid=%s", v.UID))
	}
	if v.GID != "" {
		exec.Command("addgroup", "-S", "-g", v.GID, v.GID).Run()
		cmd.Args = append(cmd.Args, "-o", fmt.Sprintf("gid=%s", v.GID))
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

	if u.User != nil {
		username := u.User.Username()
		password, _ := u.User.Password()
		cmd.Stdin = strings.NewReader(fmt.Sprintf("%s\n%s", username, password))
	} else if v.Username != "" {
		cmd.Stdin = strings.NewReader(fmt.Sprintf("%s\n%s", v.Username, v.Password))
	}

	logrus.Debug(cmd.Args)
	return cmd.Run()
}

func (d *webdavfsDriver) unmountVolume(target string) error {
	cmd := fmt.Sprintf("umount %s", target)
	logrus.Debug(cmd)
	return exec.Command("sh", "-c", cmd).Run()
}

func logError(format string, args ...interface{}) error {
	logrus.Errorf(format, args...)
	return fmt.Errorf(format, args)
}

func main() {
	debug := os.Getenv("DEBUG")
	if ok, _ := strconv.ParseBool(debug); ok {
		logrus.SetLevel(logrus.DebugLevel)
	}

	// make sure "/etc/webdavfs2/secrets" is owned by root
	err := os.Chown("/etc/webdavfs2/secrets", 0, 0)
	if err != nil {
		log.Fatal(err)
	}

	d, err := newwebdavfsDriver("/mnt")
	if err != nil {
		log.Fatal(err)
	}
	h := volume.NewHandler(d)
	logrus.Infof("listening on %s", socketAddress)
	logrus.Error(h.ServeUnix(socketAddress, 0))
}
