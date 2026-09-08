package repository

import "golang.org/x/mod/semver"

func isStableVersion(version string) bool {
	return semver.IsValid(version) && semver.Canonical(version) == version && semver.Prerelease(version) == "" && semver.Build(version) == ""
}
