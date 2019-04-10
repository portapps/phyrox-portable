//go:generate go install -v github.com/kevinburke/go-bindata/go-bindata
//go:generate go-bindata -prefix res/ -pkg assets -o assets/assets.go res/Firefox.lnk
//go:generate go install -v github.com/josephspurrier/goversioninfo/cmd/goversioninfo
//go:generate goversioninfo -icon=res/papp.ico
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"

	_ "github.com/kevinburke/go-bindata"
	"github.com/portapps/firefox-portable/assets"
	. "github.com/portapps/portapps"
	"github.com/portapps/portapps/pkg/dialog"
	"github.com/portapps/portapps/pkg/mutex"
	"github.com/portapps/portapps/pkg/shortcut"
	"github.com/portapps/portapps/pkg/utl"
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
	app *App
	cfg *config
)

func init() {
	var err error

	// Default config
	cfg = &config{
		Profile:               "default",
		MultipleInstances:     false,
		DisableTelemetry:      false,
		DisableFirefoxStudies: false,
	}

	// Init app
	if app, err = NewWithCfg("firefox-portable", "Firefox", cfg); err != nil {
		Log.Fatal().Err(err).Msg("Cannot initialize application. See log file for more info.")
	}
}

func main() {
	utl.CreateFolder(app.DataPath)
	profileFolder := utl.CreateFolder(app.DataPath, "profile", cfg.Profile)

	app.Process = utl.PathJoin(app.AppPath, "firefox.exe")
	app.Args = []string{
		"--profile",
		profileFolder,
	}

	// Multiple instances
	if cfg.MultipleInstances {
		Log.Info().Msg("Multiple instances enabled")
		app.Args = append(app.Args, "--no-remote")
	}

	// Policies
	distributionFolder := utl.CreateFolder(app.AppPath, "distribution")
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
		Log.Fatal().Msg("Cannot marshal policies")
	}
	if err = ioutil.WriteFile(utl.PathJoin(distributionFolder, "policies.json"), rawPolicies, 0644); err != nil {
		Log.Fatal().Msg("Cannot write policies")
	}

	// Set env vars
	crashreporterFolder := utl.CreateFolder(app.DataPath, "crashreporter")
	pluginsFolder := utl.CreateFolder(app.DataPath, "plugins")
	utl.OverrideEnv("MOZ_CRASHREPORTER", "0")
	utl.OverrideEnv("MOZ_CRASHREPORTER_DATA_DIRECTORY", crashreporterFolder)
	utl.OverrideEnv("MOZ_CRASHREPORTER_DISABLE", "1")
	utl.OverrideEnv("MOZ_CRASHREPORTER_NO_REPORT", "1")
	utl.OverrideEnv("MOZ_DATA_REPORTING", "0")
	utl.OverrideEnv("MOZ_MAINTENANCE_SERVICE", "0")
	utl.OverrideEnv("MOZ_PLUGIN_PATH", pluginsFolder)
	utl.OverrideEnv("MOZ_UPDATER", "0")

	// Create and check mutex
	mu, err := mutex.New(app.ID)
	defer mu.Release()
	if err != nil {
		if !cfg.MultipleInstances {
			Log.Error().Msg("You have to enable multiple instances in your configuration if you want to launch another instance")
			if _, err = dialog.MsgBox(
				fmt.Sprintf("%s portable", app.Name),
				"Other instance detected. You have to enable multiple instances in your configuration if you want to launch another instance.",
				dialog.MsgBoxBtnOk|dialog.MsgBoxIconError); err != nil {
				Log.Error().Err(err).Msg("Cannot create dialog box")
			}
			return
		} else {
			Log.Warn().Msg("Another instance is already running")
		}
	}

	// Copy default shortcut
	shortcutPath := path.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Firefox Portable.lnk")
	defaultShortcut, err := assets.Asset("Firefox.lnk")
	if err != nil {
		Log.Error().Err(err).Msg("Cannot load asset Firefox.lnk")
	}
	err = ioutil.WriteFile(shortcutPath, defaultShortcut, 0644)
	if err != nil {
		Log.Error().Err(err).Msg("Cannot write default shortcut")
	}

	// Update default shortcut
	err = shortcut.Create(shortcut.Shortcut{
		ShortcutPath:     shortcutPath,
		TargetPath:       app.Process,
		Arguments:        shortcut.Property{Clear: true},
		Description:      shortcut.Property{Value: "Firefox Portable by Portapps"},
		IconLocation:     shortcut.Property{Value: app.Process},
		WorkingDirectory: shortcut.Property{Value: app.AppPath},
	})
	if err != nil {
		Log.Error().Err(err).Msg("Cannot create shortcut")
	}
	defer func() {
		if err := os.Remove(shortcutPath); err != nil {
			Log.Error().Err(err).Msg("Cannot remove shortcut")
		}
	}()

	app.Launch(os.Args[1:])
}
