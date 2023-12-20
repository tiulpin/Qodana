/*
 * Copyright 2021-2023 JetBrains s.r.o.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package platform

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/pterm/pterm"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Lower a shortcut to strings.ToLower.
func Lower(s string) string {
	return strings.ToLower(s)
}

// Contains checks if a string is in a given slice.
func Contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

// getHash returns a SHA256 hash of a given string.
func getHash(s string) string {
	sha256sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sha256sum[:])
}

// Append appends a string to a slice if it's not already there.
//
//goland:noinspection GoUnnecessarilyExportedIdentifiers
func Append(slice []string, elems ...string) []string {
	if !Contains(slice, elems[0]) {
		slice = append(slice, elems[0])
	}
	return slice
}

// CheckDirFiles checks if a directory contains files.
func CheckDirFiles(dir string) bool {
	files, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(files) > 0
}

// FindFiles returns a slice of files with the given extensions from the given root (recursive).
func FindFiles(root string, extensions []string) []string {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		fileExtension := filepath.Ext(path)
		if Contains(extensions, fileExtension) {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	return files
}

// QuoteIfSpace wraps in '"' if '`s`' Contains space.
func QuoteIfSpace(s string) string {
	if strings.Contains(s, " ") {
		return "\"" + s + "\""
	} else {
		return s
	}
}

// QuoteForWindows wraps in '"' if '`s`' contains space on windows.
func QuoteForWindows(s string) string {
	if //goland:noinspection GoBoolExpressions
	strings.Contains(s, " ") && runtime.GOOS == "windows" {
		return "\"" + s + "\""
	} else {
		return s
	}
}

// getRemoteUrl returns remote url of the current git repository.
func getRemoteUrl() string {
	url := os.Getenv(QodanaRemoteUrl)
	if url == "" {
		out, err := exec.Command("git", "remote", "get-url", "origin").Output()
		if err != nil {
			return ""
		}
		url = string(out)
	}
	return strings.TrimSpace(url)
}

// GetDeviceIdSalt set consistent device.id based on given repo upstream #SA-391.
func GetDeviceIdSalt() []string {
	salt := os.Getenv("SALT")
	deviceId := os.Getenv("DEVICEID")
	if salt == "" || deviceId == "" {
		hash := "00000000000000000000000000000000"
		remoteUrl := getRemoteUrl()
		if remoteUrl != "" {
			hash = fmt.Sprintf("%x", md5.Sum(append([]byte("1n1T-$@Lt-"), remoteUrl...)))
		}
		if salt == "" {
			salt = fmt.Sprintf("%x", md5.Sum([]byte("$eC0nd-$@Lt-"+hash)))
		}
		if deviceId == "" {
			deviceId = fmt.Sprintf("200820300000000-%s-%s-%s-%s", hash[0:4], hash[4:8], hash[8:12], hash[12:24])
		}
	}
	return []string{deviceId, salt}
}

// IsContainer checks if Qodana is running in a container.
func IsContainer() bool {
	return os.Getenv(QodanaDockerEnv) != ""
}

func getJavaExecutablePath() (string, error) {
	var java string
	var err error
	var ret int
	//goland:noinspection GoBoolExpressions
	if runtime.GOOS == "windows" {
		java, _, ret, err = RunCmdRedirectOutput("", "java -XshowSettings:properties -version 2>&1 | findstr java.home")
	} else {
		java, _, ret, err = RunCmdRedirectOutput("", "java -XshowSettings:properties -version 2>&1 | grep java.home")
	}
	if err != nil || ret != 0 {
		return "", fmt.Errorf("failed to get JAVA_HOME: %w, %d. Check that java executable is accessible from the PATH", err, ret)
	}
	split := strings.Split(java, "=")
	if len(split) < 2 {
		return "", fmt.Errorf("failed to get JAVA_HOME: %s. Check that java executable is accessible from the PATH", java)
	}

	javaHome := split[1]
	javaHome = strings.Trim(javaHome, "\r\n ")

	var javaExecFileName string
	//goland:noinspection GoBoolExpressions
	if runtime.GOOS == "windows" {
		javaExecFileName = "java.exe"
	} else {
		javaExecFileName = "java"
	}

	javaExecutablePath := filepath.Join(javaHome, "bin", javaExecFileName)
	return javaExecutablePath, nil
}

// LaunchAndLog launches a process and logs its output.
func LaunchAndLog(opts *QodanaOptions, executable string, args ...string) (int, error) {
	stdout, stderr, ret, err := RunCmdRedirectOutput("", args...)
	if err != nil {
		log.Error(fmt.Errorf("failed to run %s: %w", executable, err))
		return ret, err
	}
	fmt.Println(stdout)
	if stderr != "" {
		log.Error(stderr)
	}
	if err := AppendToFile(filepath.Join(opts.LogDirPath(), executable+"-out.log"), stdout); err != nil {
		log.Error(err)
	}
	if err := AppendToFile(filepath.Join(opts.LogDirPath(), executable+"-err.log"), stderr); err != nil {
		log.Error(err)
	}
	return ret, nil
}

// DownloadFile downloads a file from a given url to a given filepath.
func DownloadFile(filepath string, url string, spinner *pterm.SpinnerPrinter) error {
	response, err := http.Head(url)
	if err != nil {
		return err
	}
	size, _ := strconv.Atoi(response.Header.Get("Content-Length"))

	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Fatalf("Error while closing HTTP stream: %v", err)
		}
	}(resp.Body)

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}

	defer func(out *os.File) {
		err := out.Close()
		if err != nil {
			log.Fatalf("Error while closing output file: %v", err)
		}
	}(out)

	buffer := make([]byte, 1024)
	total := 0
	lastTotal := 0
	text := ""
	if spinner != nil {
		text = spinner.Text
	}
	for {
		length, err := resp.Body.Read(buffer)
		if err != nil && err != io.EOF {
			return err
		}
		total += length
		if spinner != nil && total-lastTotal > 1024*1024 {
			lastTotal = total
			spinner.UpdateText(fmt.Sprintf("%s (%d %%)", text, 100*total/size))
		}
		if length == 0 {
			break
		}
		if _, err = out.Write(buffer[:length]); err != nil {
			return err
		}
	}

	if total != size {
		return fmt.Errorf("downloaded file size doesn't match expected size")
	}

	if spinner != nil {
		spinner.UpdateText(fmt.Sprintf("%s (100 %%)", text))
	}

	return nil
}

// reverse reverses the given string slice.
func reverse(s []string) []string {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}
