package httpx

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPrettyAssetSpecUsesReadableTemplateLines(t *testing.T) {
	raw := json.RawMessage(`{
		"template":"# 接待用语\n\n您好，{{ .name }}",
		"variables":[{"name":"name","type":"string","description":"客户姓名","required":true,"default":"朋友"}]
	}`)
	text, err := prettyAssetSpec(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"模板内容\n# 接待用语\n\n您好，{{ .name }}\n",
		"变量设置",
		"- name | 类型: 文本 | 必填: 是 | 说明: 客户姓名 | 默认值: \"朋友\"",
		"模型服务\n未绑定\n",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("formatted spec missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, `# 接待用语\n\n您好`) {
		t.Fatalf("template newlines are still escaped: %s", text)
	}
}
