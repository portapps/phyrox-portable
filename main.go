//go:generate go install -v github.com/josephspurrier/goversioninfo/cmd/goversioninfo
//go:generate goversioninfo -icon=res/papp.ico
package main

import (
	"encoding/json"
	"io/ioutil"
	"os"

	. "github.com/portapps/portapps"
)

type config struct {
	Profile               string `yaml:"profile"`
	MultipleInstances     bool   `yaml:"multiple_instances"`
	DisableTelemetry      bool   `yaml:"disable_telemetry"`
	DisableFirefoxStudies bool   `yaml:"disable_firefox_studies"`
}

type policies struct {
	DisableAppUpdate        bool `json:"DisableAppUpdate"`
	DisableFirefoxStudies   bool `json:"DisableFirefoxStudies"`
	DisableTelemetry        bool `json:"DisableTelemetry"`
	DontCheckDefaultBrowser bool `json:"DontCheckDefaultBrowser"`
}

var (
	cfg = config{
		Profile:               "default",
		MultipleInstances:     false,
		DisableTelemetry:      false,
		DisableFirefoxStudies: false,
	}
)

func init() {
	Papp.ID = "firefox-portable"
	Papp.Name = "Firefox"
	InitWithCfg(&cfg)
}

func main() {
	Papp.AppPath = AppPathJoin("app")
	Papp.DataPath = CreateFolder(AppPathJoin("data"))

	Papp.Process = PathJoin(Papp.AppPath, "firefox.exe")
	Papp.WorkingDir = Papp.AppPath
	Papp.Args = []string{
		"--profile",
		PathJoin(Papp.DataPath, "profile", cfg.Profile),
	}

	// Multiple instances
	if cfg.MultipleInstances {
		Papp.Args = append(Papp.Args, "--no-remote")
	}

	// Policies
	distributionFolder := CreateFolder(PathJoin(Papp.AppPath, "distribution"))
	policies := struct {
		policies `json:"policies"`
	}{
		policies{
			DisableAppUpdate:        true,
			DisableFirefoxStudies:   cfg.DisableFirefoxStudies,
			DisableTelemetry:        cfg.DisableTelemetry,
			DontCheckDefaultBrowser: true,
		},
	}
	rawPolicies, err := json.MarshalIndent(policies, "", "  ")
	if err != nil {
		Log.Fatal("Cannot marshal policies:", err)
	}
	if err = ioutil.WriteFile(PathJoin(distributionFolder, "policies.json"), rawPolicies, 0644); err != nil {
		Log.Fatal("Cannot write policies:", err)
	}

	// Set env vars
	OverrideEnv("MOZ_CRASHREPORTER", "0")
	OverrideEnv("MOZ_CRASHREPORTER_DATA_DIRECTORY", CreateFolder(PathJoin(Papp.DataPath, "crashreporter")))
	OverrideEnv("MOZ_CRASHREPORTER_DISABLE", "1")
	OverrideEnv("MOZ_CRASHREPORTER_NO_REPORT", "1")
	OverrideEnv("MOZ_DATA_REPORTING", "0")
	OverrideEnv("MOZ_MAINTENANCE_SERVICE", "0")
	OverrideEnv("MOZ_PLUGIN_PATH", CreateFolder(PathJoin(Papp.DataPath, "plugins")))
	OverrideEnv("MOZ_UPDATER", "0")

	Launch(os.Args[1:])
}
