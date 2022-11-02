package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cavaliergopher/grab/v3"
)

type BackupSettings struct {
	SourceAddr string `json:"source_addr"` // 文件系统地址
	TargetPath string `json:"target_path"` // 本地备份地址
	Suffix     string `json:"suffix"`      // 备份文件后缀
	Internal   int    `json:"internal"`    // 备份间隔 单位小时
	Storage    int    `json:"storage"`     // 备份文件数量上限
}

func (b *BackupSettings) GetLocalFiles() (filenames []string, err error) {
	dir, err := ioutil.ReadDir(b.TargetPath)
	if err != nil {
		return nil, err
	}

	for _, fi := range dir {
		// 过滤指定格式
		if ok := strings.HasSuffix(fi.Name(), "."+b.Suffix); ok {
			filenames = append(filenames, fi.Name())
		}
	}

	sort.Strings(filenames)
	return filenames, nil
}

func (b *BackupSettings) GetRemoteFiles() (filenames []string, err error) {
	request, err := http.NewRequest(http.MethodGet, b.SourceAddr, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return
	}
	s, _ := ioutil.ReadAll(resp.Body)
	re := regexp.MustCompile(`>(.*\.` + b.Suffix + `?)<`)
	matches := re.FindAllStringSubmatch(string(s), -1)
	for _, match := range matches {
		if len(match) == 2 {
			filenames = append(filenames, match[1])
		}
	}

	sort.Strings(filenames)
	return
}

func (b *BackupSettings) DownloadFiles(filenames []string) (err error) {
	for _, filename := range filenames {
		_, itemErr := grab.Get(b.TargetPath+string(os.PathSeparator)+filename, b.SourceAddr+"/"+filename)
		if itemErr != nil {
			deleteFile(b.TargetPath, filename)
			log.Println(itemErr)
		}
		if err == nil {
			err = itemErr
		}
	}
	return
}

func (b *BackupSettings) Run() {
	for {
		localFilenames, err := b.GetLocalFiles()
		if err != nil {
			log.Println(err)
		}
		remoteFilenames, err := b.GetRemoteFiles()
		if err != nil {
			log.Println(err)
		}
		incrementalFilenames := compareFiles(localFilenames, remoteFilenames)
		if b.Storage > 0 {
			oldestFileIdx := len(localFilenames) + len(incrementalFilenames) - b.Storage
			var downloadErr error
			if oldestFileIdx <= len(localFilenames) {
				downloadErr = b.DownloadFiles(incrementalFilenames)
			} else {
				downloadErr = b.DownloadFiles(incrementalFilenames[oldestFileIdx-len(localFilenames):])
			}
			if downloadErr == nil {
				for i := 0; i < oldestFileIdx && i < len(localFilenames); i++ {
					deleteFile(b.TargetPath, localFilenames[i])
				}
			}

		} else {
			b.DownloadFiles(incrementalFilenames)
		}
		if b.Internal == 0 {
			return
		}
		time.Sleep(time.Duration(b.Internal) * time.Hour)
	}
}

func deleteFile(path, filename string) error {
	return os.Remove(path + string(os.PathSeparator) + filename)
}

func compareFiles(localFilenames, remoteFilenames []string) (incrementalFilenames []string) {
	if len(localFilenames) == 0 {
		return remoteFilenames
	}
	latestLocalFilename := localFilenames[len(localFilenames)-1]
	for _, remoteFilename := range remoteFilenames {
		if remoteFilename > latestLocalFilename {
			incrementalFilenames = append(incrementalFilenames, remoteFilename)
		}
	}
	return
}

func main() {
	settings := make([]BackupSettings, 0)
	configFile, err := os.Open("./config/config.json")
	if err != nil {
		log.Println(err)
		return
	}
	byteValue, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(byteValue, &settings)
	for _, setting := range settings {
		go func(s BackupSettings) {
			s.Run()
		}(setting)
	}
	select {}
}
