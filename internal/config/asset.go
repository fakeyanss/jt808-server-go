// Code generated for package config by go-bindata DO NOT EDIT. (@generated)
// sources:
// configs/banner.txt
// configs/default.yaml
// test/client/configs/default.yaml
package config

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bindataRead(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _configsBannerTxt = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xa4\x4f\x41\xaa\x44\x21\x0c\xdb\x7b\x8a\x2c\x15\x3e\xf4\x42\x42\xfe\x41\x7a\xf8\xa1\x4d\x2b\xe2\x72\x26\x60\x4c\x3a\x69\x9c\x07\x81\x20\x01\xf2\xe5\xaf\x30\x74\x4d\x2e\x18\x67\x34\x2f\x0b\xde\xd2\xf5\x62\xa1\x55\x8f\xce\xd5\xa1\xf2\x2a\x35\x44\x53\xb6\xa5\x96\x07\x3c\x43\xa1\x83\xb0\x4b\xb9\x02\xed\x4f\x08\xff\xfa\x43\xa3\x3b\x8d\x75\x6e\xce\xf8\x8c\xfa\x85\x7c\xd5\x00\x38\x3c\x45\xf9\xee\xbc\xd6\x86\x7e\xdb\x64\x9c\x87\xf5\x15\x67\x60\x9a\xf8\xe3\x13\x9b\xfc\x43\xaf\x0d\x3b\xdb\xbf\xc0\x78\xd7\x7c\x02\x00\x00\xff\xff\x45\xef\x01\xe6\xfd\x01\x00\x00")

func configsBannerTxtBytes() ([]byte, error) {
	return bindataRead(
		_configsBannerTxt,
		"configs/banner.txt",
	)
}

func configsBannerTxt() (*asset, error) {
	bytes, err := configsBannerTxtBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "configs/banner.txt", size: 509, mode: os.FileMode(420), modTime: time.Unix(1678162039, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _configsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x6c\xd0\xc1\x4a\x03\x31\x10\x06\xe0\xfb\x3e\xc5\x90\x7b\x77\xb7\x82\x18\x72\x6b\x69\x15\x44\xb0\x28\x3e\x40\x1a\x67\xd3\xe8\x34\xb3\x24\xb3\xa5\xfa\xf4\x12\xa3\xb8\xa2\xc7\x7c\xff\x64\x7e\x12\x62\x6f\x1a\x00\xc7\x31\x33\xe1\x36\xda\x3d\xa1\x01\x49\x13\x36\x00\x43\xf8\x43\x63\x0a\x51\x56\xf9\x36\x73\x34\x30\x58\xca\x05\x89\xfd\x1d\x9e\x90\x0c\xa8\xcd\x76\xfd\x74\xa3\xaa\x6d\x42\x42\x27\x9c\xde\x0c\xa8\xb6\x23\xf6\xb9\xfb\x4a\xae\x43\x59\xa9\x5e\x44\xf7\x7a\x91\x31\x9d\x30\x2d\x3c\xb7\xc4\xbe\x0c\x1c\xed\xf9\x31\xbc\xe3\xfd\xf0\xc0\x44\x21\x7a\x03\x97\x7d\xe5\xb5\x75\xaf\xd3\x98\x67\xc9\xf2\x42\xd7\x68\xe5\xe7\x17\xae\x9a\xa6\xae\x2d\x8f\x8b\xf6\xf8\x4f\x5b\x69\x1a\x39\x49\x99\x00\x10\x37\xee\xca\x01\x94\xee\x75\xaf\x3e\x6d\x7a\x9e\xd9\xb2\xda\x41\xe4\x07\x7b\x5d\x70\x6f\x63\xac\x45\x00\xf8\xfb\xb7\xbe\xc3\x9d\x95\x83\x01\xe5\x38\x0e\xc1\xe7\xae\x62\x2b\x67\x51\xcd\x47\x00\x00\x00\xff\xff\x4d\x9b\x24\x20\x81\x01\x00\x00")

func configsDefaultYamlBytes() ([]byte, error) {
	return bindataRead(
		_configsDefaultYaml,
		"configs/default.yaml",
	)
}

func configsDefaultYaml() (*asset, error) {
	bytes, err := configsDefaultYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "configs/default.yaml", size: 385, mode: os.FileMode(420), modTime: time.Unix(1678936086, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _testClientConfigsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x84\x94\xbd\x72\xdb\x38\x10\xc7\x7b\x3e\x05\x86\xae\x25\x93\xfa\x38\x51\xe8\xe4\xcf\xf1\x8d\x67\xce\x63\xfb\xae\x38\x8d\x0b\x18\x5c\xc1\x38\x41\x58\x0e\x00\x6a\x4e\x77\xf6\x1b\xc4\x45\x8a\x74\xc9\xa4\x4d\x91\x32\x55\x3e\x26\x2f\x63\xbb\xcd\x2b\x64\x48\x90\x8a\x64\x9b\x72\xc9\xfd\xfd\xf7\xbf\xc0\x2e\x96\x0a\x05\x0d\x08\xe1\xa8\x2d\x2a\xd8\xd7\xec\x52\x01\x25\xce\xe4\x10\x10\x32\x91\x4f\x42\x99\x91\xda\x8d\xec\xef\x16\x35\x25\x13\xa6\x6c\x11\x54\x28\x8e\x61\x0e\x8a\x92\x70\x6f\x7f\xe7\xcf\xc3\xd0\xc7\xf6\xa4\x01\xee\xd0\x2c\x28\x09\xdb\xdb\x0a\x85\xdd\xae\xc8\x81\x2c\x2c\xc3\x7f\x5c\x12\x25\x2d\xae\x24\x68\xd7\x12\xd8\x56\x28\x0a\xc1\x8c\xfd\x7b\x26\xff\x83\x3f\x26\xa7\xa8\x94\xd4\x82\x92\x7e\xe4\xc3\x3b\x8c\x4f\xf3\xcc\xae\x90\xb8\x93\x78\x34\x12\xab\x09\x83\x20\xf0\xb6\xc5\xe5\x34\x9b\x3d\x53\x2d\xf4\xd7\xe6\xb9\x31\xa0\xf9\x82\x92\xd8\x07\x74\x91\x42\x88\x81\x19\x3a\x18\xa5\xa9\xa1\x24\x54\xc8\x99\xba\x42\xeb\x68\x12\x25\x51\x58\x0a\xb6\xd6\x25\x71\x3c\x6c\x77\x7a\xc3\x76\x6f\xd8\x8e\xe3\x88\xf6\x2b\x5d\x0a\x73\xc9\xc1\x5b\xca\xf4\x14\x04\x25\xe1\x38\x6a\x0d\x2f\xfe\x1f\xdc\x78\x1f\x39\x03\xb9\x1a\x8f\xfb\x37\x75\x81\xec\x0a\x35\x78\x16\x77\x23\x8f\x93\x8a\xae\xb3\xb8\xd3\xed\xf5\x7f\x1b\x24\xcb\x4c\xc5\x5c\x45\xef\x3e\x7f\x1c\x8d\x47\xad\xbf\xcb\xec\xda\xfb\x11\x2f\xd3\x2b\x62\xd0\x21\x47\xf5\x17\x18\x2b\x8b\x21\x87\x9d\x28\xee\x7a\xe6\x0c\xd3\xf6\xa4\x10\x50\x12\x9e\xef\x9e\xf8\xe8\x14\x20\x63\x4a\xce\x81\x92\x4e\x44\xb6\xc8\xdd\xf7\x77\x0f\x9f\xbe\xdc\xbf\xfe\xf0\xf0\xf6\xfd\x8f\xaf\xaf\xee\x6f\xdf\xdc\x7d\xbb\xb5\xb5\xf9\x5c\x6a\x0e\x47\x6b\x9d\xe8\x54\x87\xe2\xd2\x2d\xd6\x49\x6f\xf5\xb8\xbb\xa8\xd0\xd4\xb4\x3c\x71\xd1\xad\x9b\x5f\x5d\x3e\x04\xf4\x8d\x2e\xc6\xe5\x24\xea\x53\xc8\xd0\xb8\x23\xed\xc0\xcc\x99\xa2\x24\x8e\x4a\x2c\x6a\x1d\x21\x8c\xf3\x33\xc7\x5c\x6e\xbd\x6f\x74\x1d\x87\x15\xa9\x3d\x9a\x30\x73\xd2\xe5\x29\x9c\x2f\x32\x78\x26\x57\x8b\x66\x8a\x19\x18\xe6\xa4\x16\x0d\xd6\x02\x70\x5f\x73\xb3\xc8\x36\x95\x47\x96\x36\xa0\x83\x1c\xd4\xd9\xc2\x3a\x98\x35\x08\x46\xca\x81\xd1\xcc\xa1\xd9\x28\xdb\x43\x34\xc7\xc8\xa7\xd0\x54\x68\x62\x50\xbb\x42\xd5\xc0\x67\x32\xdd\x40\x2f\x19\x9f\x6e\xc0\xa9\x91\x73\x30\x1b\x04\x3c\xb7\x0e\x67\x1b\x04\x22\xb3\xc7\x9b\x67\x78\x09\x32\xc5\xfc\x05\x91\x50\xa8\x99\x7d\xc9\x4a\x30\x25\x15\xe0\x0b\xaa\xe2\x52\x4d\x73\xaf\xdf\x1b\x7d\xf4\xc0\x96\xeb\x90\x5c\x94\x3b\x71\x3d\x8c\x9e\x3c\xb3\xf5\x65\xba\x8e\xc7\x51\x6b\x50\xa9\xe3\x64\x29\x67\xca\x3d\x51\xd7\x0b\x56\xb6\xbb\x2e\x6d\x33\x80\x67\x17\x94\x90\xb4\xfc\xa5\x97\xab\xb5\x56\x72\x1c\xb7\x3a\x17\xcb\xaf\xee\x38\x6a\xf5\xfd\x67\x18\x04\x55\x6a\x10\xfc\x0c\x00\x00\xff\xff\x4b\x85\x47\xbb\x6c\x06\x00\x00")

func testClientConfigsDefaultYamlBytes() ([]byte, error) {
	return bindataRead(
		_testClientConfigsDefaultYaml,
		"test/client/configs/default.yaml",
	)
}

func testClientConfigsDefaultYaml() (*asset, error) {
	bytes, err := testClientConfigsDefaultYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "test/client/configs/default.yaml", size: 1644, mode: os.FileMode(420), modTime: time.Unix(1679488008, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"configs/banner.txt":               configsBannerTxt,
	"configs/default.yaml":             configsDefaultYaml,
	"test/client/configs/default.yaml": testClientConfigsDefaultYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"configs": &bintree{nil, map[string]*bintree{
		"banner.txt":   &bintree{configsBannerTxt, map[string]*bintree{}},
		"default.yaml": &bintree{configsDefaultYaml, map[string]*bintree{}},
	}},
	"test": &bintree{nil, map[string]*bintree{
		"client": &bintree{nil, map[string]*bintree{
			"configs": &bintree{nil, map[string]*bintree{
				"default.yaml": &bintree{testClientConfigsDefaultYaml, map[string]*bintree{}},
			}},
		}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
