package cli

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

// buildFileCmdTree mirrors the relevant slice of the public command tree so we
// can drive canonicalCommandID without booting the full registry.
func buildFileCmdTree() (root, instance, file, upload, download *cobra.Command) {
	root = &cobra.Command{Use: "agr"}
	instance = &cobra.Command{Use: "instance"}
	file = &cobra.Command{Use: "file"}
	upload = &cobra.Command{Use: "upload"}
	download = &cobra.Command{Use: "download"}
	root.AddCommand(instance)
	instance.AddCommand(file)
	file.AddCommand(upload)
	file.AddCommand(download)
	return
}

var _ = Describe("commandSpecificUsageHint", func() {
	It("returns positional usage hint for unknown flag on instance.file.upload", func() {
		_, _, _, upload, _ := buildFileCmdTree()
		err := errors.New("unknown flag: --source")
		Expect(commandSpecificUsageHint(upload, err)).To(Equal(
			"upload uses positional paths; use: agr instance file upload <instance-id> <local-path|-> <remote-path>",
		))
	})

	It("returns positional usage hint for unknown shorthand on instance.file.upload", func() {
		_, _, _, upload, _ := buildFileCmdTree()
		err := errors.New("unknown shorthand flag: 's' in -s")
		Expect(commandSpecificUsageHint(upload, err)).To(Equal(
			"upload uses positional paths; use: agr instance file upload <instance-id> <local-path|-> <remote-path>",
		))
	})

	It("returns positional usage hint for unknown flag on instance.file.download", func() {
		_, _, _, _, download := buildFileCmdTree()
		err := errors.New("unknown flag: --target")
		Expect(commandSpecificUsageHint(download, err)).To(Equal(
			"download uses positional paths; use: agr instance file download <instance-id> <remote-path> <local-path|->",
		))
	})

	It("returns empty for non unknown-flag errors on file commands", func() {
		_, _, _, upload, _ := buildFileCmdTree()
		err := errors.New("requires at least 3 arg(s), got 1")
		Expect(commandSpecificUsageHint(upload, err)).To(BeEmpty())
	})

	It("returns empty for unknown flag on unrelated commands", func() {
		root := &cobra.Command{Use: "agr"}
		other := &cobra.Command{Use: "other"}
		root.AddCommand(other)
		err := errors.New("unknown flag: --source")
		Expect(commandSpecificUsageHint(other, err)).To(BeEmpty())
	})

	It("returns empty for nil cmd or nil error", func() {
		_, _, _, upload, _ := buildFileCmdTree()
		Expect(commandSpecificUsageHint(nil, errors.New("unknown flag: --source"))).To(BeEmpty())
		Expect(commandSpecificUsageHint(upload, nil)).To(BeEmpty())
	})
})
