package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/manifest"
	"github.com/spf13/cobra"
)

// DiffManifestsOptions 包含diff-manifests命令的选项
type DiffManifestsOptions struct {
	Raw         bool
	NoColor     bool
	PrettyFormat string
	Verbose     bool
	Quiet       bool
	OuterManifest bool
	NoOuterManifest bool
	ThisManifestOnly bool
	AllManifests bool
	Name       bool
	Path       bool
	Revision   bool
	Groups     bool
	All        bool
	XML        bool
}

// DiffManifestsCmd 返回diff-manifests命令
func DiffManifestsCmd() *cobra.Command {
	opts := &DiffManifestsOptions{}

	cmd := &cobra.Command{
		Use:   "diffmanifests manifest1.xml [manifest2.xml]",
		Short: "Show differences between project revisions of manifests",
		Long:  `The repo diffmanifests command shows differences between project revisions of
manifest1 and manifest2. if manifest2 is not specified, current manifest.xml
will be used instead. Both absolute and relative paths may be used for
manifests. Relative paths start from project's ".repo/manifests" folder.

The --raw option Displays the diff in a way that facilitates parsing, the
project pattern will be <status> <path> <revision from> [<revision to>] and the
commit pattern will be <status> <onelined log> with status values respectively :

  A = Added project
  R = Removed project
  C = Changed project
  U = Project with unreachable revision(s) (revision(s) not found)

for project, and

   A = Added commit
   R = Removed commit

for a commit.

Only changed projects may contain commits, and commit status always starts with
a space, and are part of last printed project. Unreachable revisions may occur
if project is not up to date or if repo has not been initialized with all the
groups, in which case some projects won't be synced and their revisions won't be
available.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiffManifests(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVar(&opts.Raw, "raw", false, "display raw diff")
	cmd.Flags().BoolVar(&opts.NoColor, "no-color", false, "does not display the diff in color")
	cmd.Flags().StringVar(&opts.PrettyFormat, "pretty-format", "", "print the log using a custom git pretty format string")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")
	cmd.Flags().BoolVar(&opts.AllManifests, "all-manifests", false, "operate on this manifest and its submanifests")
	cmd.Flags().BoolVarP(&opts.Name, "name", "n", false, "diff project names only")
	cmd.Flags().BoolVarP(&opts.Path, "path", "p", false, "diff project paths only")
	cmd.Flags().BoolVarP(&opts.Revision, "revision", "r", false, "diff project revisions only")
	cmd.Flags().BoolVarP(&opts.Groups, "groups", "g", false, "diff project groups only")
	cmd.Flags().BoolVarP(&opts.All, "all", "a", true, "diff all project attributes")
	cmd.Flags().BoolVarP(&opts.XML, "xml", "x", false, "diff raw XML content")

	return cmd
}

// runDiffManifests 执行diff-manifests命令
func runDiffManifests(opts *DiffManifestsOptions, args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("exactly two manifest files required")
	}

	manifest1Path := args[0]
	manifest2Path := args[1]

	fmt.Printf("Comparing manifests %s and %s\n", manifest1Path, manifest2Path)

	// 创建清单解析器
	parser := manifest.NewParser()

	// 解析第一个清单文件
	manifest1, err := parser.ParseFromFile(manifest1Path)
	if err != nil {
		return fmt.Errorf("failed to parse first manifest: %w", err)
	}

	// 解析第二个清单文件
	manifest2, err := parser.ParseFromFile(manifest2Path)
	if err != nil {
		return fmt.Errorf("failed to parse second manifest: %w", err)
	}

	// 如果选择了XML比较
	if opts.XML {
		return diffManifestsXML(manifest1Path, manifest2Path)
	}

	// 比较清单
	diffs := compareManifests(manifest1, manifest2, opts)

	// 显示差异
	if len(diffs) == 0 {
		fmt.Println("No differences found between the manifests")
	} else {
		fmt.Println("Differences found:")
		for _, diff := range diffs {
			fmt.Println(diff)
		}
	}

	return nil
}

// diffManifestsXML 比较两个清单文件的原始XML内容
func diffManifestsXML(manifest1Path, manifest2Path string) error {
	// 这里应该实现XML文件比较逻辑
	// 可以使用外部命令如diff或者内部实现的XML比较
	
	// 示例：使用简单的文件内容比较
	fmt.Println("XML comparison not implemented yet")
	
	return nil
}

// compareManifests 比较两个清单对象并返回差异列表
func compareManifests(manifest1, manifest2 *manifest.Manifest, opts *DiffManifestsOptions) []string {
	diffs := []string{}
	
	// 比较默认远程仓库
	if manifest1.Default.Remote != manifest2.Default.Remote {
		if opts.Raw {
			diffs = append(diffs, fmt.Sprintf("C default %s %s", manifest1.Default.Remote, manifest2.Default.Remote))
		} else {
			diffs = append(diffs, fmt.Sprintf("Default remote: %s -> %s", 
				manifest1.Default.Remote, manifest2.Default.Remote))
		}
	}
	
	// 比较默认修订版本
	if manifest1.Default.Revision != manifest2.Default.Revision {
		if opts.Raw {
			diffs = append(diffs, fmt.Sprintf("C default %s %s", manifest1.Default.Revision, manifest2.Default.Revision))
		} else {
			diffs = append(diffs, fmt.Sprintf("Default revision: %s -> %s", 
				manifest1.Default.Revision, manifest2.Default.Revision))
		}
	}
	
	// 创建项目映射以便于比较
	projects1 := make(map[string]manifest.Project)
	for _, p := range manifest1.Projects {
		projects1[p.Name] = p
	}
	
	projects2 := make(map[string]manifest.Project)
	for _, p := range manifest2.Projects {
		projects2[p.Name] = p
	}
	
	// 检查项目1中存在但项目2中不存在的项目
	for name := range projects1 {
		if _, exists := projects2[name]; !exists {
			if opts.Raw {
				diffs = append(diffs, fmt.Sprintf("R %s", name))
			} else {
				diffs = append(diffs, fmt.Sprintf("Project removed: %s", name))
			}
		}
	}
	
	// 检查项目2中存在但项目1中不存在的项目
	for name, p2 := range projects2 {
		p1, exists := projects1[name]
		if !exists {
			if opts.Raw {
				diffs = append(diffs, fmt.Sprintf("A %s", name))
			} else {
				diffs = append(diffs, fmt.Sprintf("Project added: %s", name))
			}
			continue
		}
		
		// 比较项目属性
		if (opts.Path || opts.All) && p1.Path != p2.Path {
			if opts.Raw {
				diffs = append(diffs, fmt.Sprintf("C %s %s %s", name, p1.Path, p2.Path))
			} else {
				diffs = append(diffs, fmt.Sprintf("Project %s path: %s -> %s", 
					name, p1.Path, p2.Path))
			}
		}
		
		if (opts.Revision || opts.All) && p1.Revision != p2.Revision {
			if opts.Raw {
				diffs = append(diffs, fmt.Sprintf("C %s %s %s", name, p1.Revision, p2.Revision))
			} else {
				diffs = append(diffs, fmt.Sprintf("Project %s revision: %s -> %s", 
					name, p1.Revision, p2.Revision))
			}
		}
		
		if (opts.Groups || opts.All) && p1.Groups != p2.Groups {
			if opts.Raw {
				diffs = append(diffs, fmt.Sprintf("C %s %s %s", name, p1.Groups, p2.Groups))
			} else {
				diffs = append(diffs, fmt.Sprintf("Project %s groups: %s -> %s", 
					name, p1.Groups, p2.Groups))
			}
		}
	}
	
	return diffs
}