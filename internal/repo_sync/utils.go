package repo_sync

// import (
// 	// Keep necessary imports like "strings" if used elsewhere
// 	"strings"
// 	// Remove unused imports like "github.com/leopardxu/repo-go/internal/git"
// 	// Remove unused imports like "github.com/leopardxu/repo-go/internal/manifest"
// 	// Remove unused imports like "github.com/leopardxu/repo-go/internal/project"
// )

// contains 检查字符串切片是否包含指定字符串
func contains(slice []string, str string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}

// difference 返回在a中但不在b中的元素
func difference(a, b []string) []string {
	result := []string{}
	for _, item := range a {
		if !contains(b, item) {
			result = append(result, item)
		}
	}
	return result
}

// Removed convertManifestProject function

// Removed getRemoteURL function
