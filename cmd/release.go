// Copyright © 2016 Prometheus Team
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/progrium/go-shell"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/JeremyEinfeld/promu/util/retry"
)

var (
	githubRelease  = shell.Cmd("github-release").ErrFn()
	allowedRetries int
)

// releaseCmd represents the release command
var releaseCmd = &cobra.Command{
	Use:   "release [<tarballs-location>]",
	Short: "Upload tarballs to the Github release",
	Long:  `Upload tarballs to the Github release`,
	Run: func(cmd *cobra.Command, args []string) {
		tarballsLocation := optArg(args, 0, ".")
		runRelease(tarballsLocation)
	},
}

// init prepares cobra flags
func init() {
	Promu.AddCommand(releaseCmd)

	releaseCmd.Flags().IntVar(&allowedRetries, "retry", 2, "Number of retries to perform when upload fails")
}

func runRelease(tarballsLocation string) {
	defer shell.ErrExit()
	shell.Tee = os.Stdout

	if viper.GetBool("verbose") {
		shell.Trace = true
	}

	if err := filepath.Walk(tarballsLocation, releaseTarball); err != nil {
		fatalMsg("Failed to upload all tarballs", err)
	}
}

func releaseTarball(path string, f os.FileInfo, err error) error {
	fileName := filepath.Base(path)
	tarPattern := fmt.Sprintf("%s-%s.*.tar.gz", info.Name, info.Version)

	matched, err := regexp.MatchString(tarPattern, fileName)
	if err != nil {
		return err
	}

	if matched {
		err := retry.Do(func(attempt int) (bool, error) {
			var err error
			err = uploadTarball(fileName, path)
			if err != nil {
				time.Sleep(2 * time.Second)
			}
			return attempt < allowedRetries+1, err
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Upload failed after %d attempts\n", allowedRetries+1)
			return err
		}
		fmt.Println(" > uploaded", fileName)
	}

	return nil
}

func uploadTarball(fileName string, path string) error {
	return githubRelease("upload",
		"--user", info.Owner,
		"--repo", info.Name,
		"--tag", fmt.Sprintf("v%s", info.Version),
		"--name", fileName,
		"--file", path)
}
