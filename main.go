//go:generate go install -v github.com/josephspurrier/goversioninfo/cmd/goversioninfo
//go:generate goversioninfo -icon=res/papp.ico
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	. "github.com/portapps/portapps"
	"github.com/portapps/portapps/pkg/dialog"
	"github.com/portapps/portapps/pkg/mutex"
)

type config struct {
	Profile               string `yaml:"profile" mapstructure:"profile"`
	MultipleInstances     bool   `yaml:"multiple_instances" mapstructure:"multiple_instances"`
	DisableTelemetry      bool   `yaml:"disable_telemetry" mapstructure:"disable_telemetry"`
	DisableFirefoxStudies bool   `yaml:"disable_firefox_studies" mapstructure:"disable_firefox_studies"`
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

	profileFolder := CreateFolder(PathJoin(Papp.DataPath, "profile", cfg.Profile))

	Papp.Process = PathJoin(Papp.AppPath, "firefox.exe")
	Papp.WorkingDir = Papp.AppPath
	Papp.Args = []string{
		"--profile",
		profileFolder,
	}

	// Multiple instances
	if cfg.MultipleInstances {
		Log.Info("Multiple instances enabled")
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

	// Create and check mutex
	mu, err := mutex.New(Papp.ID, Log)
	defer mu.Release()
	if err != nil {
		if !cfg.MultipleInstances {
			Log.Error("You have to enable multiple instances in your configuration if you want to launch multiple instances")
			if _, err = dialog.MsgBox(
				fmt.Sprintf("%s portable", Papp.Name),
				fmt.Sprintf("You have to enable multiple instances in your configuration if you want to launch multiple instances of %s Portable", Papp.Name),
				dialog.MsgBoxBtnOk|dialog.MsgBoxIconError); err != nil {
				Log.Error("Cannot create dialog box", err)
			}
			return
		} else {
			Log.Warning("Another instance is already running:", err)
		}
	}

	Launch(os.Args[1:])
}
