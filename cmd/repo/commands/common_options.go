package commands

import "github.com/spf13/cobra"

// CommonManifestOptions 包含清单相关的选项
type CommonManifestOptions struct {
	Groups                  string
	Platform                bool
	OuterManifest           bool
	NoOuterManifest         bool
	ThisManifestOnly        bool
	AllManifests            bool
	RevisionAsHEAD          bool
	OutputFile              string
	SuppressUpstreamRevision bool
	SuppressDestBranch      bool
	Snapshot                bool
	NoCloneBundle           bool
	JsonOutput              bool
	PrettyOutput            bool
	NoLocalManifests        bool
}

// AddManifestFlags 添加多清单选项到命�?
func AddManifestFlags(cmd *cobra.Command, opts *CommonManifestOptions) {
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")
	cmd.Flags().BoolVar(&opts.AllManifests, "all-manifests", false, "operate on this manifest and its submanifests")
}
