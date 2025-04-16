package manifest

import (
	"testing"
)

func TestCustomAttributes(t *testing.T) {
	// 包含自定义属性的XML
	xmlData := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<manifest custom-attr="manifest-value">
  <remote name="origin" fetch="https://example.com/" custom-remote-attr="remote-value" />
  <default remote="origin" revision="master" custom-default-attr="default-value" />
  <project name="project1" path="path/to/project1" custom-project-attr="project-value">
    <copyfile src="src/file" dest="dest/file" custom-copyfile-attr="copyfile-value" />
    <linkfile src="src/link" dest="dest/link" custom-linkfile-attr="linkfile-value" />
  </project>
  <include name="other.xml" custom-include-attr="include-value" />
  <remove-project name="removed-project" custom-remove-attr="remove-value" />
</manifest>`)

	// 解析XML
	parser := NewParser()
	manifest, err := parser.Parse(xmlData)
	if err != nil {
		t.Fatalf("Failed to parse manifest: %v", err)
	}

	// 测试Manifest自定义属性
	if val, ok := manifest.GetCustomAttr("custom-attr"); !ok || val != "manifest-value" {
		t.Errorf("Expected manifest custom-attr='manifest-value', got '%s', exists=%v", val, ok)
	}

	// 测试Remote自定义属性
	if len(manifest.Remotes) == 0 {
		t.Fatalf("No remotes found")
	}
	if val, ok := manifest.Remotes[0].GetCustomAttr("custom-remote-attr"); !ok || val != "remote-value" {
		t.Errorf("Expected remote custom-remote-attr='remote-value', got '%s', exists=%v", val, ok)
	}

	// 测试Default自定义属性
	if val, ok := manifest.Default.GetCustomAttr("custom-default-attr"); !ok || val != "default-value" {
		t.Errorf("Expected default custom-default-attr='default-value', got '%s', exists=%v", val, ok)
	}

	// 测试Project自定义属性
	if len(manifest.Projects) == 0 {
		t.Fatalf("No projects found")
	}
	if val, ok := manifest.Projects[0].GetCustomAttr("custom-project-attr"); !ok || val != "project-value" {
		t.Errorf("Expected project custom-project-attr='project-value', got '%s', exists=%v", val, ok)
	}

	// 测试Copyfile自定义属性
	if len(manifest.Projects[0].Copyfiles) == 0 {
		t.Fatalf("No copyfiles found")
	}
	if val, ok := manifest.Projects[0].Copyfiles[0].GetCustomAttr("custom-copyfile-attr"); !ok || val != "copyfile-value" {
		t.Errorf("Expected copyfile custom-copyfile-attr='copyfile-value', got '%s', exists=%v", val, ok)
	}

	// 测试Linkfile自定义属性
	if len(manifest.Projects[0].Linkfiles) == 0 {
		t.Fatalf("No linkfiles found")
	}
	if val, ok := manifest.Projects[0].Linkfiles[0].GetCustomAttr("custom-linkfile-attr"); !ok || val != "linkfile-value" {
		t.Errorf("Expected linkfile custom-linkfile-attr='linkfile-value', got '%s', exists=%v", val, ok)
	}

	// 测试Include自定义属性
	if len(manifest.Includes) == 0 {
		t.Fatalf("No includes found")
	}
	if val, ok := manifest.Includes[0].GetCustomAttr("custom-include-attr"); !ok || val != "include-value" {
		t.Errorf("Expected include custom-include-attr='include-value', got '%s', exists=%v", val, ok)
	}

	// 测试RemoveProject自定义属性
	if len(manifest.RemoveProjects) == 0 {
		t.Fatalf("No remove-projects found")
	}
	if val, ok := manifest.RemoveProjects[0].GetCustomAttr("custom-remove-attr"); !ok || val != "remove-value" {
		t.Errorf("Expected remove-project custom-remove-attr='remove-value', got '%s', exists=%v", val, ok)
	}
}