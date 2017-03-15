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
	"strings"

	"github.com/mcuadros/go-version"
	"github.com/progrium/go-shell"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/JeremyEinfeld/promu/util/sh"
)

var (
	dockerBuilderImageName = "quay.io/prometheus/golang-builder"

	defaultMainPlatforms = []string{
		"linux/amd64", "linux/386", "darwin/amd64", "darwin/386", "windows/amd64", "windows/386",
		"freebsd/amd64", "freebsd/386", "openbsd/amd64", "openbsd/386", "netbsd/amd64", "netbsd/386",
		"dragonfly/amd64",
	}
	defaultARMPlatforms = []string{
		"linux/arm", "linux/arm64", "freebsd/arm", "openbsd/arm", "netbsd/arm",
	}
	defaultPowerPCPlatforms = []string{
		"linux/ppc64", "linux/ppc64le",
	}
	defaultMIPSPlatforms = []string{
		"linux/mips64", "linux/mips64le",
	}
)

// crossbuildCmd represents the crossbuild command
var crossbuildCmd = &cobra.Command{
	Use:   "crossbuild",
	Short: "Crossbuild a Go project using Golang builder Docker images",
	Long:  `Crossbuild a Go project using Golang builder Docker images`,
	Run: func(cmd *cobra.Command, args []string) {
		runCrossbuild()
	},
	PreRun: func(cmd *cobra.Command, args []string) {
		if err := hasRequiredConfigurations("repository.path"); err != nil {
			fatal(err)
		}
	},
}

// init prepares cobra flags
func init() {
	Promu.AddCommand(crossbuildCmd)

	crossbuildCmd.Flags().Bool("cgo", false, "Enable CGO using several docker images with different crossbuild toolchains.")
	crossbuildCmd.Flags().String("go", "", "Golang builder version to use")
	crossbuildCmd.Flags().StringP("platforms", "p", "", "Platforms to build")

	viper.BindPFlag("crossbuild.platforms", crossbuildCmd.Flags().Lookup("platforms"))
	viper.BindPFlag("go.cgo", crossbuildCmd.Flags().Lookup("cgo"))
	viper.BindPFlag("go.version", crossbuildCmd.Flags().Lookup("go"))

	// Current bug in viper: SeDefault doesn't work with nested key
	// viper.SetDefault("go.version", "1.7.1")
	// platforms := defaultMainPlatforms
	// platforms = append(platforms, defaultARMPlatforms...)
	// platforms = append(platforms, defaultPowerPCPlatforms...)
	// platforms = append(platforms, defaultMIPSPlatforms...)
	// viper.SetDefault("crossbuild.platforms", platforms)
}

func runCrossbuild() {
	defer shell.ErrExit()
	shell.Tee = os.Stdout

	if viper.GetBool("verbose") {
		shell.Trace = true
	}

	var (
		mainPlatforms    []string
		armPlatforms     []string
		powerPCPlatforms []string
		mipsPlatforms    []string
		unknownPlatforms []string

		cgo       = viper.GetBool("go.cgo")
		goVersion = viper.GetString("go.version")
		repoPath  = viper.GetString("repository.path")
		platforms = viper.GetStringSlice("crossbuild.platforms")

		dockerBaseBuilderImage    = fmt.Sprintf("%s:%s-base", dockerBuilderImageName, goVersion)
		dockerMainBuilderImage    = fmt.Sprintf("%s:%s-main", dockerBuilderImageName, goVersion)
		dockerARMBuilderImage     = fmt.Sprintf("%s:%s-arm", dockerBuilderImageName, goVersion)
		dockerPowerPCBuilderImage = fmt.Sprintf("%s:%s-powerpc", dockerBuilderImageName, goVersion)
		dockerMIPSBuilderImage    = fmt.Sprintf("%s:%s-mips", dockerBuilderImageName, goVersion)
	)

	for _, platform := range platforms {
		switch {
		case stringInSlice(platform, defaultMainPlatforms):
			mainPlatforms = append(mainPlatforms, platform)
		case stringInSlice(platform, defaultARMPlatforms):
			armPlatforms = append(armPlatforms, platform)
		case stringInSlice(platform, defaultPowerPCPlatforms):
			powerPCPlatforms = append(powerPCPlatforms, platform)
		case stringInSlice(platform, defaultMIPSPlatforms):
			if version.Compare(goVersion, "1.6", ">=") {
				mipsPlatforms = append(mipsPlatforms, platform)
			} else {
				warn(fmt.Errorf("MIPS architectures are only available with Go 1.6+"))
			}
		default:
			unknownPlatforms = append(unknownPlatforms, platform)
		}
	}

	if len(unknownPlatforms) > 0 {
		warn(fmt.Errorf("unknown/unhandled platforms: %s", unknownPlatforms))
	}

	if !cgo {
		// In non-CGO, use the base image without any crossbuild toolchain
		var allPlatforms []string
		allPlatforms = append(allPlatforms, mainPlatforms[:]...)
		allPlatforms = append(allPlatforms, armPlatforms[:]...)
		allPlatforms = append(allPlatforms, powerPCPlatforms[:]...)
		allPlatforms = append(allPlatforms, mipsPlatforms[:]...)

		pg := &platformGroup{"base", dockerBaseBuilderImage, allPlatforms}
		if err := pg.Build(repoPath); err != nil {
			fatalMsg(fmt.Sprintf("The %s builder docker image exited unexpectedly", pg.Name), err)
		}
	} else {
		os.Setenv("CGO_ENABLED", "1")
		defer os.Unsetenv("CGO_ENABLED")

		for _, pg := range []platformGroup{
			platformGroup{"main", dockerMainBuilderImage, mainPlatforms},
			platformGroup{"ARM", dockerARMBuilderImage, armPlatforms},
			platformGroup{"PowerPC", dockerPowerPCBuilderImage, powerPCPlatforms},
			platformGroup{"MIPS", dockerMIPSBuilderImage, mipsPlatforms},
		} {
			if err := pg.Build(repoPath); err != nil {
				fatalMsg(fmt.Sprintf("The %s builder docker image exited unexpectedly", pg.Name), err)
			}
		}
	}
}

type platformGroup struct {
	Name        string
	DockerImage string
	Platforms   []string
}

func (pg platformGroup) Build(repoPath string) error {
	if platformsParam := strings.Join(pg.Platforms[:], " "); platformsParam != "" {
		fmt.Printf("> running the %s builder docker image\n", pg.Name)
		if err := docker("run --rm -t -v $PWD:/app", pg.DockerImage, "-i", repoPath, "-p", sh.Quote(platformsParam)); err != nil {
			return err
		}
	}
	return nil
}
