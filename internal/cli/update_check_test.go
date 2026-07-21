package cli

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("extractCommandToken", func() {
	DescribeTable("extracts the first positional subcommand",
		func(args []string, expected string) {
			Expect(extractCommandToken(args)).To(Equal(expected))
		},
		Entry("simple subcommand", []string{"instance", "list"}, "instance"),
		Entry("flags before subcommand", []string{"--config", "foo.toml", "update"}, "update"),
		Entry("-o flag before subcommand", []string{"-o", "json", "status"}, "status"),
		Entry("--output=json before subcommand", []string{"--output=json", "status"}, "status"),
		Entry("-ojson shorthand before subcommand", []string{"-ojson", "status"}, "status"),
		Entry("boolean flags before subcommand", []string{"--debug", "--no-color", "tool"}, "tool"),
		Entry("--version flag only", []string{"--version"}, ""),
		Entry("-v flag only", []string{"-v"}, ""),
		Entry("--help flag only", []string{"--help"}, ""),
		Entry("empty args", []string{}, ""),
		Entry("multiple valued flags", []string{"--region", "ap-guangzhou", "--secret-id", "xxx", "instance"}, "instance"),
		Entry("-- stops parsing", []string{"--", "instance"}, ""),
		Entry("--config=value format", []string{"--config=custom.toml", "tool"}, "tool"),
	)
})

var _ = Describe("shouldRunUpdateCheck", func() {
	// Save and restore global state that shouldRunUpdateCheck reads.
	var savedNonInteractive bool

	BeforeEach(func() {
		savedNonInteractive = nonInteractive
		nonInteractive = false
	})
	AfterEach(func() {
		nonInteractive = savedNonInteractive
	})

	DescribeTable("determines whether to run background update check",
		func(args []string, expected bool) {
			Expect(shouldRunUpdateCheck(args)).To(Equal(expected))
		},
		Entry("normal command", []string{"instance", "list"}, true),
		Entry("bare agr (no subcommand)", []string{}, true),
		Entry("update command skipped", []string{"update"}, false),
		Entry("version command skipped", []string{"version"}, false),
		Entry("help command skipped", []string{"help"}, false),
		Entry("--version flag skipped", []string{"--version"}, false),
		Entry("-v flag skipped", []string{"-v"}, false),
		Entry("--help flag skipped", []string{"--help"}, false),
		Entry("-h flag skipped", []string{"-h"}, false),
		Entry("--config before version", []string{"--config", "c.toml", "version"}, false),
		Entry("--config before normal cmd", []string{"--config", "c.toml", "tool"}, true),
		Entry("-v among other flags (no subcommand)", []string{"--debug", "-v"}, false),
		Entry("--version after --config (no subcommand)", []string{"--config", "c.toml", "--version"}, false),
		Entry("-v after subcommand is not misinterpreted", []string{"tool", "create", "-v"}, true),
	)

	It("returns false when nonInteractive is set", func() {
		nonInteractive = true
		Expect(shouldRunUpdateCheck([]string{"instance", "list"})).To(BeFalse())
	})
})
