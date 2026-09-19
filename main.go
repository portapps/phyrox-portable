package main

//go:generate go tool goversioninfo -icon=res/papp.ico -manifest=res/papp.manifest

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"github.com/portapps/portapps/v3"
	"github.com/portapps/portapps/v3/pkg/files"
	"github.com/portapps/portapps/v3/pkg/log"
	"github.com/portapps/portapps/v3/pkg/mutex"
	"github.com/portapps/portapps/v3/pkg/shortcut"
	"github.com/portapps/portapps/v3/pkg/win"
)

//go:embed res/Firefox.lnk
var defaultShortcut []byte

type config struct {
	Profile              string `yaml:"profile" mapstructure:"profile"`
	MultipleInstances    bool   `yaml:"multiple_instances" mapstructure:"multiple_instances"`
	DisableTelemetry     bool   `yaml:"disable_telemetry" mapstructure:"disable_telemetry"`
	DisableCrashReporter bool   `yaml:"disable_crash_reporter" mapstructure:"disable_crash_reporter"`
	Locale               string `yaml:"locale" mapstructure:"locale"`
	Cleanup              bool   `yaml:"cleanup" mapstructure:"cleanup"`
}

var (
	app *portapps.App
	cfg *config
)

const (
	defaultLocale = "en-US"
)

func init() {
	var err error

	// Default config
	cfg = &config{
		Profile:              "default",
		MultipleInstances:    false,
		DisableTelemetry:     false,
		DisableCrashReporter: true,
		Locale:               defaultLocale,
		Cleanup:              false,
	}

	// Init app
	if app, err = portapps.NewWithCfg("phyrox-portable", "Phyrox", cfg); err != nil {
		log.Fatal().Err(err).Msg("Cannot initialize application. See log file for more info.")
	}
}

func main() {
	profileFolder := filepath.Join(app.DataPath, "profile", cfg.Profile)
	for _, dir := range []string{app.DataPath, profileFolder} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal().Err(err).Msgf("Cannot create directory %s", dir)
		}
	}

	app.Process = filepath.Join(app.AppPath, "firefox.exe")
	app.Args = []string{
		"-profile",
		profileFolder,
	}

	// Set env vars
	crashreporterFolder := filepath.Join(app.DataPath, "crashreporter")
	pluginsFolder := filepath.Join(app.DataPath, "plugins")
	for _, dir := range []string{crashreporterFolder, pluginsFolder} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal().Err(err).Msgf("Cannot create directory %s", dir)
		}
	}
	os.Setenv("MOZ_CRASHREPORTER_DATA_DIRECTORY", crashreporterFolder)
	os.Setenv("MOZ_MAINTENANCE_SERVICE", "0")
	os.Setenv("MOZ_PLUGIN_PATH", pluginsFolder)
	os.Setenv("MOZ_UPDATER", "0")
	if cfg.DisableCrashReporter {
		os.Setenv("MOZ_CRASHREPORTER", "0")
		os.Setenv("MOZ_CRASHREPORTER_DISABLE", "1")
		os.Setenv("MOZ_CRASHREPORTER_NO_REPORT", "1")
	}
	if cfg.DisableTelemetry {
		os.Setenv("MOZ_DATA_REPORTING", "0")
	}

	// Create and check mutex
	mu, err := mutex.Create(app.ID)
	if err != nil {
		if !cfg.MultipleInstances {
			log.Error().Msg("You have to enable multiple instances in your configuration if you want to launch another instance")
			if _, err = win.MsgBox(
				fmt.Sprintf("%s portable", app.Name),
				"Other instance detected. You have to enable multiple instances in your configuration if you want to launch another instance.",
				win.MsgBoxBtnOk|win.MsgBoxIconError); err != nil {
				log.Error().Err(err).Msg("Cannot create dialog box")
			}
			return
		} else {
			log.Warn().Msg("Another instance is already running")
		}
	} else {
		defer mutex.Release(mu)
	}

	// Cleanup on exit
	if cfg.Cleanup {
		defer func() {
			var paths []string
			if appData := os.Getenv("APPDATA"); appData != "" {
				paths = append(paths, filepath.Join(appData, "Mozilla", "Firefox"))
			}
			if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
				paths = append(paths, filepath.Join(localAppData, "Mozilla", "Firefox"))
			}
			if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
				paths = append(paths, filepath.Join(userProfile, "AppData", "LocalLow", "Thunderbird"))
			}
			files.Cleanup(paths...)
		}()
	}

	// Locale
	locale, err := checkLocale()
	if err != nil {
		log.Error().Err(err).Msg("Cannot set locale")
	}

	// Multiple instances
	if cfg.MultipleInstances {
		log.Info().Msg("Multiple instances enabled")
		app.Args = append(app.Args, "-no-remote")
	}

	// Policies
	if err := createPolicies(locale); err != nil {
		log.Fatal().Err(err).Msg("Cannot create policies")
	}

	// Autoconfig
	prefFolder := filepath.Join(app.AppPath, "defaults/pref")
	if err := os.MkdirAll(prefFolder, 0o755); err != nil {
		log.Fatal().Err(err).Msg("Cannot create preferences directory")
	}
	autoconfig := filepath.Join(prefFolder, "autoconfig.js")
	if err := os.WriteFile(autoconfig, []byte(`//
pref("general.config.filename", "portapps.cfg");
pref("general.config.obscure_value", 0);`), 0o644); err != nil {
		log.Fatal().Err(err).Msg("Cannot write autoconfig.js")
	}

	// Mozilla cfg
	mozillaCfgPath := filepath.Join(app.AppPath, "portapps.cfg")
	mozillaCfgFile, err := os.Create(mozillaCfgPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Cannot create portapps.cfg")
	}
	mozillaCfgData := struct {
		DisableCrashReporter bool
		Locale               string
	}{
		cfg.DisableCrashReporter,
		strconv.Quote(locale),
	}
	mozillaCfgTpl := template.Must(template.New("mozillaCfg").Parse(`// Portable defaults only.

// Locale fallback. Prefer policies.json RequestedLocales for modern Firefox.
pref("intl.locale.requested", {{ .Locale }});

// Keep first-run noise down.
pref("browser.rights.3.shown", true);
pref("browser.startup.homepage_override.mstone", "ignore");

{{ if .DisableCrashReporter -}}
// Disable crash reporter
lockPref("toolkit.crashreporter.enabled", false);
{{ end -}}
`))
	if err := mozillaCfgTpl.Execute(mozillaCfgFile, mozillaCfgData); err != nil {
		mozillaCfgFile.Close()
		log.Fatal().Err(err).Msg("Cannot write portapps.cfg")
	}
	if err := mozillaCfgFile.Close(); err != nil {
		log.Fatal().Err(err).Msg("Cannot close portapps.cfg")
	}

	// Fix extensions path
	if err := updateAddonStartup(profileFolder); err != nil {
		log.Error().Err(err).Msg("Cannot fix extensions path")
	}

	// Copy default shortcut
	shortcutPath := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Phyrox Portable.lnk")
	err = os.WriteFile(shortcutPath, defaultShortcut, 0644)
	if err != nil {
		log.Error().Err(err).Msg("Cannot write default shortcut")
	}

	// Update default shortcut
	err = shortcut.Create(shortcut.Shortcut{
		ShortcutPath:     shortcutPath,
		TargetPath:       app.Process,
		Arguments:        shortcut.Property{Clear: true},
		Description:      shortcut.Property{Value: "Phyrox Portable by Portapps"},
		IconLocation:     shortcut.Property{Value: app.Process},
		WorkingDirectory: shortcut.Property{Value: app.AppPath},
	})
	if err != nil {
		log.Error().Err(err).Msg("Cannot create shortcut")
	}
	defer func() {
		if err := os.Remove(shortcutPath); err != nil {
			log.Error().Err(err).Msg("Cannot remove shortcut")
		}
	}()

	defer app.Close()
	app.Launch(os.Args[1:])
}

func createPolicies(locale string) error {
	distributionPath := filepath.Join(app.AppPath, "distribution")
	if err := os.MkdirAll(distributionPath, 0o755); err != nil {
		return errors.Wrap(err, "Cannot create distribution directory")
	}
	appFile := filepath.Join(distributionPath, "policies.json")
	dataFile := filepath.Join(app.DataPath, "policies.json")
	jsonPolicies := map[string]interface{}{
		"policies": map[string]interface{}{},
	}
	defaultPolicies, err := json.Marshal(jsonPolicies)
	if err != nil {
		return errors.Wrap(err, "Cannot marshal default policies")
	}
	log.Debug().Msgf("Default policies: %s", string(defaultPolicies))

	if files.Exists(dataFile) {
		rawCustomPolicies, err := os.ReadFile(dataFile)
		if err != nil {
			return errors.Wrap(err, "Cannot read custom policies")
		}

		if err := json.Unmarshal(rawCustomPolicies, &jsonPolicies); err != nil {
			return errors.Wrap(err, "Cannot consume custom policies")
		}
		customPolicies, err := json.Marshal(jsonPolicies)
		if err != nil {
			return errors.Wrap(err, "Cannot marshal custom policies")
		}
		log.Debug().Msgf("Custom policies: %s", string(customPolicies))
	}

	policies, ok := jsonPolicies["policies"].(map[string]interface{})
	if !ok {
		if _, exists := jsonPolicies["policies"]; exists {
			return errors.New("policies must be an object")
		}
		policies = map[string]interface{}{}
		jsonPolicies["policies"] = policies
	}
	policies["DisableAppUpdate"] = true
	policies["DontCheckDefaultBrowser"] = true
	if cfg.DisableTelemetry {
		policies["DisableFirefoxStudies"] = true
		policies["DisableTelemetry"] = true
	}
	if locale != "" {
		policies["RequestedLocales"] = locale
	}

	appliedPolicies, err := json.MarshalIndent(jsonPolicies, "", "  ")
	if err != nil {
		return errors.Wrap(err, "Cannot marshal policies")
	}
	log.Debug().Msgf("Applied policies: %s", string(appliedPolicies))
	if err := os.WriteFile(appFile, appliedPolicies, 0644); err != nil {
		return errors.Wrap(err, "Cannot write policies")
	}

	return nil
}

func checkLocale() (string, error) {
	extSourceFile := fmt.Sprintf("%s.xpi", cfg.Locale)
	extDestFile := fmt.Sprintf("langpack-%s@firefox.mozilla.org.xpi", cfg.Locale)
	extsFolder := filepath.Join(app.AppPath, "distribution", "extensions")
	if err := os.MkdirAll(extsFolder, 0o755); err != nil {
		return defaultLocale, errors.Wrap(err, "Cannot create extensions directory")
	}
	localeXpi := filepath.Join(app.AppPath, "langs", extSourceFile)

	// If default locale skip (already embedded)
	if cfg.Locale == defaultLocale {
		return cfg.Locale, nil
	}

	// Check .xpi file exists
	if !files.Exists(localeXpi) {
		return defaultLocale, fmt.Errorf("XPI file does not exist in %s", localeXpi)
	}

	// Copy .xpi
	if err := files.CopyFile(localeXpi, filepath.Join(extsFolder, extDestFile)); err != nil {
		return defaultLocale, err
	}

	return cfg.Locale, nil
}

func updateAddonStartup(profileFolder string) error {
	lz4File := filepath.Join(profileFolder, "addonStartup.json.lz4")
	if !files.Exists(lz4File) || app.Prev.RootPath == "" {
		return nil
	}

	lz4Raw, err := mozLz4Decompress(lz4File)
	if err != nil {
		return err
	}

	prevPathLin := escapedUnixPath(app.Prev.RootPath)
	currPathLin := escapedUnixPath(app.RootPath)
	lz4Str := strings.ReplaceAll(string(lz4Raw), prevPathLin, currPathLin)

	prevPathWin := escapedWindowsPath(app.Prev.RootPath)
	currPathWin := escapedWindowsPath(app.RootPath)
	lz4Str = strings.ReplaceAll(lz4Str, prevPathWin, currPathWin)

	lz4Enc, err := mozLz4Compress([]byte(lz4Str))
	if err != nil {
		return err
	}

	return os.WriteFile(lz4File, lz4Enc, 0644)
}

func escapedUnixPath(path string) string {
	return strings.ReplaceAll(filepath.ToSlash(path), ` `, `%20`)
}

func escapedWindowsPath(path string) string {
	return strings.ReplaceAll(strings.ReplaceAll(filepath.FromSlash(path), `\`, `\\`), ` `, `%20`)
}
