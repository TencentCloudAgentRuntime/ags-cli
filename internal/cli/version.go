package cli

import (
	"fmt"
	"io"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/TencentCloudAgentRuntime/ags-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	// Version is the build version printed by "agr version".
	Version = "dev"
	// Commit identifies the source revision used to build the binary.
	Commit = "unknown"
	// BuildTime records when the binary was built.
	BuildTime = "unknown"
)

var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print version information",
	Long:    `Print the version, commit hash, and build time of AGR CLI.`,
	Example: exampleBlocks("agr version", "agr --version", "agr version -o json"),
}

func init() {
	versionCmd.RunE = Wrap("version", versionFn)
	rootCmd.AddCommand(versionCmd)
}

func versionFn(cmd *cobra.Command, args []string) (*CmdResult, error) {
	version, commit, buildTime := resolvedVersionInfo()
	data := &output.VersionData{
		Version:   version,
		Commit:    commit,
		BuildTime: buildTime,
	}
	return OK(data, func(w io.Writer) {
		fmt.Fprintf(w, "agr version %s\n", version)
		fmt.Fprintf(w, "  commit: %s\n", commit)
		fmt.Fprintf(w, "  built:  %s\n", buildTime)
	}), nil
}

// SetVersionInfo overrides build metadata, usually from main via ldflags or
// tests that need deterministic version output.
func SetVersionInfo(version, commit, buildTime string) {
	if version != "" {
		Version = version
	}
	if commit != "" {
		Commit = commit
	}
	if buildTime != "" {
		BuildTime = buildTime
	}
}

func resolvedVersionInfo() (string, string, string) {
	version, commit, buildTime := Version, Commit, BuildTime
	if bi, ok := debug.ReadBuildInfo(); ok {
		if (version == "" || version == "dev") && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			version = bi.Main.Version
		}
		if commit == "" || commit == "unknown" {
			for _, setting := range bi.Settings {
				if setting.Key == "vcs.revision" && setting.Value != "" {
					commit = setting.Value
				}
				if setting.Key == "vcs.time" && setting.Value != "" && (buildTime == "" || buildTime == "unknown") {
					buildTime = setting.Value
				}
			}
		}
		// Fallback: extract commit and timestamp from pseudo-version
		// (e.g. "v0.0.0-20260527094209-6eb0623826b9") when VCS info
		// is unavailable — common for binaries installed via go install.
		if commit == "" || commit == "unknown" {
			if c, t := parsePseudoVersion(bi.Main.Version); c != "" {
				commit = c
				if (buildTime == "" || buildTime == "unknown") && t != "" {
					buildTime = t
				}
			}
		}
	}
	// Last-resort fallback: when version came from a tagged release (i.e.
	// we recognised a real semver from build info) but commit/buildTime
	// are still the default "unknown", the binary was almost certainly
	// produced by `go install <module>@<tag>` — Go does not stamp VCS
	// metadata for module-cache builds. Replace the bare "unknown" with a
	// self-explanatory marker so users do not read it as a release
	// pipeline failure. Pre-built release binaries always inject ldflags
	// and never reach this branch.
	if version != "" && version != "dev" {
		if commit == "" || commit == "unknown" {
			commit = "n/a (go install)"
		}
		if buildTime == "" || buildTime == "unknown" {
			buildTime = "n/a (go install)"
		}
	}
	return version, commit, buildTime
}

// pseudoVersionRe matches Go module pseudo-version suffixes:
//
//	vX.Y.Z-yyyymmddhhmmss-abcdefabcdef
//	vX.Y.Z-pre.N.yyyymmddhhmmss-abcdefabcdef
var pseudoVersionRe = regexp.MustCompile(`(\d{14})-([0-9a-f]{12})$`)

// parsePseudoVersion extracts the short commit hash and RFC3339 timestamp
// from a Go module pseudo-version string. Returns empty strings if the
// version is not a pseudo-version.
func parsePseudoVersion(version string) (commit string, buildTime string) {
	m := pseudoVersionRe.FindStringSubmatch(version)
	if m == nil {
		return "", ""
	}
	commit = m[2]
	ts := m[1]
	// Parse timestamp "20060102150405" → RFC3339.
	if t, err := time.Parse("20060102150405", ts); err == nil {
		buildTime = t.UTC().Format(time.RFC3339)
	} else {
		// Return raw timestamp string on parse failure.
		buildTime = strings.Join([]string{ts[:4], ts[4:6], ts[6:8]}, "-") + "T" +
			strings.Join([]string{ts[8:10], ts[10:12], ts[12:14]}, ":") + "Z"
	}
	return commit, buildTime
}
