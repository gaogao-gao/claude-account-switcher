package main

import "testing"

func TestLanguageTags(t *testing.T) {
	if got := firstAppleLanguage("(\n    \"zh-Hans-CN\",\n    \"en-US\"\n)\n"); got != "zh-Hans-CN" {
		t.Fatalf("first language = %q", got)
	}
	for tag, want := range map[string]bool{"zh-Hans-CN": true, "zh-CN": true, "zh_CN.UTF-8": true, "en-US": false, "": false} {
		if isChinese(tag) != want {
			t.Errorf("isChinese(%q) = %v", tag, !want)
		}
	}
}
