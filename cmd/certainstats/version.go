package main

import (
	"os"
	"runtime/debug"
)

func getBuildTime() string {
	if buildTime != "" && buildTime != "unknown" {
		return buildTime
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.time" {
				return setting.Value
			}
		}
	}
	if exe, err := os.Executable(); err == nil {
		if fi, err := os.Stat(exe); err == nil {
			return fi.ModTime().UTC().Format("2006-01-02T15:04:05Z")
		}
	}
	return "unknown"
}

func getVersionAndCommit() (string, string) {
	v := version
	c := commit

	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && c == "" {
				c = setting.Value
			}
		}
		if v == "" || v == "dev" {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				v = info.Main.Version
			}
		}
	}

	if v == "" {
		v = "dev"
	}
	if c == "" {
		c = "none"
	}
	return v, c
}

func printVersion() {
	v, c := getVersionAndCommit()
	t := getBuildTime()

	if v == "dev" || v == "none" || v == "" {
		os.Stdout.WriteString(name + " dev build " + t + "\n")
	} else {
		commitSuffix := ""
		if c != "" && c != "none" {
			if len(c) > 7 {
				commitSuffix = "-" + c[:7]
			} else {
				commitSuffix = "-" + c
			}
		}
		os.Stdout.WriteString(name + " " + v + commitSuffix + " (build " + t + ")\n")
	}
}
