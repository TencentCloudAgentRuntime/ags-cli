package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("parsePseudoVersion", func() {
	It("extracts commit and timestamp from standard pseudo-version", func() {
		commit, buildTime := parsePseudoVersion("v0.0.0-20260527094209-6eb0623826b9")
		Expect(commit).To(Equal("6eb0623826b9"))
		Expect(buildTime).To(Equal("2026-05-27T09:42:09Z"))
	})

	It("extracts commit and timestamp from pre-release pseudo-version", func() {
		commit, buildTime := parsePseudoVersion("v0.5.1-0.20260101120000-abcdef123456")
		Expect(commit).To(Equal("abcdef123456"))
		Expect(buildTime).To(Equal("2026-01-01T12:00:00Z"))
	})

	It("returns empty for a proper semver tag", func() {
		commit, buildTime := parsePseudoVersion("v0.5.0")
		Expect(commit).To(BeEmpty())
		Expect(buildTime).To(BeEmpty())
	})

	It("returns empty for (devel)", func() {
		commit, buildTime := parsePseudoVersion("(devel)")
		Expect(commit).To(BeEmpty())
		Expect(buildTime).To(BeEmpty())
	})

	It("returns empty for empty string", func() {
		commit, buildTime := parsePseudoVersion("")
		Expect(commit).To(BeEmpty())
		Expect(buildTime).To(BeEmpty())
	})
})

var _ = Describe("SetVersionInfo", func() {
	var origVersion, origCommit, origBuildTime string

	BeforeEach(func() {
		origVersion = Version
		origCommit = Commit
		origBuildTime = BuildTime
	})

	AfterEach(func() {
		Version = origVersion
		Commit = origCommit
		BuildTime = origBuildTime
	})

	It("overrides all fields when non-empty values are provided", func() {
		SetVersionInfo("v1.0.0", "abc123", "2026-01-01T00:00:00Z")
		Expect(Version).To(Equal("v1.0.0"))
		Expect(Commit).To(Equal("abc123"))
		Expect(BuildTime).To(Equal("2026-01-01T00:00:00Z"))
	})

	It("does not override fields when empty strings are provided", func() {
		Version = "v0.9.0"
		Commit = "def456"
		BuildTime = "2025-12-31T23:59:59Z"
		SetVersionInfo("", "", "")
		Expect(Version).To(Equal("v0.9.0"))
		Expect(Commit).To(Equal("def456"))
		Expect(BuildTime).To(Equal("2025-12-31T23:59:59Z"))
	})
})

var _ = Describe("resolvedVersionInfo", func() {
	var origVersion, origCommit, origBuildTime string

	BeforeEach(func() {
		origVersion = Version
		origCommit = Commit
		origBuildTime = BuildTime
	})

	AfterEach(func() {
		Version = origVersion
		Commit = origCommit
		BuildTime = origBuildTime
	})

	It("substitutes 'n/a (go install)' for unknown commit/built when version is a tagged release", func() {
		// Simulates `go install <module>@v0.5.0`: ldflags do not run, so
		// the package globals stay at their compile-time defaults, but
		// debug.ReadBuildInfo reports a real semver via bi.Main.Version
		// (or, in tests, via the package global set here).
		Version = "v0.5.0"
		Commit = "unknown"
		BuildTime = "unknown"

		version, commit, buildTime := resolvedVersionInfo()
		Expect(version).To(Equal("v0.5.0"))
		Expect(commit).To(Equal("n/a (go install)"))
		Expect(buildTime).To(Equal("n/a (go install)"))
	})

	It("preserves real commit/built values injected via ldflags", func() {
		Version = "v0.5.0"
		Commit = "abcdef1"
		BuildTime = "2026-01-02T03:04:05Z"

		version, commit, buildTime := resolvedVersionInfo()
		Expect(version).To(Equal("v0.5.0"))
		Expect(commit).To(Equal("abcdef1"))
		Expect(buildTime).To(Equal("2026-01-02T03:04:05Z"))
	})

	It("does not apply the fallback when version is still the 'dev' default", func() {
		Version = "dev"
		Commit = "unknown"
		BuildTime = "unknown"

		_, commit, buildTime := resolvedVersionInfo()
		// In `go test` runs bi.Main.Version is "(devel)" so neither the
		// pseudo-version path nor the tagged-release fallback applies;
		// the literal "unknown" should remain so devs can still spot a
		// missing-ldflags build during local testing.
		Expect(commit).To(Equal("unknown"))
		Expect(buildTime).To(Equal("unknown"))
	})
})
