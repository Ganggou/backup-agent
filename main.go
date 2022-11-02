package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
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
	Username   string `json:"username"`
	Password   string `json:"password"`
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
	req, err := http.NewRequest(http.MethodGet, b.SourceAddr, nil)
	if err != nil {
		return
	}
	if b.Username != "" && b.Password != "" {
		req.Header.Add("Authorization", "Basic "+basicAuth(b.Username, b.Password))
	}
	resp, err := http.DefaultClient.Do(req)
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
		log.Println("Start getting file " + filename)
		itemErr := b.DownloadFile(filename)
		if itemErr != nil {
			log.Printf("Fail to get file %s, err: %s \n", filename, itemErr)
			deleteFile(b.TargetPath, filename)
		} else {
			log.Println("Succeed to get file " + filename)
		}
		if err == nil {
			err = itemErr
		}
	}
	return
}

func (b *BackupSettings) DownloadFile(filename string) error {
	client := grab.NewClient()
	req, _ := grab.NewRequest(b.TargetPath+string(os.PathSeparator)+filename, b.SourceAddr+"/"+filename)

	if b.Username != "" && b.Password != "" {
		req.HTTPRequest.Header.Add("Authorization", "Basic "+basicAuth(b.Username, b.Password))
	}
	resp := client.Do(req)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

	go func() {
		for range t.C {
			fmt.Printf(" transferred %s %v / %v bytes (%.2f%%)\n",
				filename,
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())
		}
	}()

	return resp.Err()
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

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

func main() {
	settings := make([]BackupSettings, 0)
	configFilename := os.Args[1]
	configFile, err := os.Open(configFilename)
	if err != nil {
		log.Println(err)
		return
	}
	byteValue, _ := ioutil.ReadAll(configFile)
	json.Unmarshal(byteValue, &settings)
	fmt.Println(settings)
	for _, setting := range settings {
		go func(s BackupSettings) {
			s.Run()
		}(setting)
	}
	select {}
}
