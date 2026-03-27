package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "列出已索引的仓库",
	Long: `列出所有已索引的代码仓库及其基本信息。

示例:
  sourcelex list`,
	RunE: runList,
}

func runList(cmd *cobra.Command, args []string) error {
	cfg := GetConfig()

	entries, err := os.ReadDir(cfg.Paths.DataDir)
	if err != nil {
		return fmt.Errorf("读取数据目录失败: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "仓库ID\t仓库URL/路径\t分支\t索引时间")
	fmt.Fprintln(w, "------\t----------\t----\t--------")

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(cfg.Paths.DataDir, entry.Name())
		meta, err := loadRepoMetadata(dir)
		if err != nil {
			continue
		}

		source := meta.RepoURL
		if source == "" {
			source = meta.RepoPath
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			meta.RepoID,
			source,
			meta.Branch,
			meta.IndexedAt.Format("2006-01-02 15:04:05"),
		)
		count++
	}

	w.Flush()

	if count == 0 {
		fmt.Println("\n尚未索引任何仓库。使用 `sourcelex store --repo <URL>` 开始索引。")
	} else {
		fmt.Printf("\n共 %d 个已索引仓库\n", count)
		fmt.Println("使用 `sourcelex serve --repo <仓库ID>` 启动指定仓库的服务")
	}

	return nil
}
