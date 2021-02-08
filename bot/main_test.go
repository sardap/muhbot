package main

import (
	"fmt"
	"strings"
	"testing"
)

type regCase struct {
	str    string
	target string
}

func TestRegex(t *testing.T) {
	testMuh := func(msg string) string {
		matches := meRe.FindAllStringSubmatchIndex(strings.ToLower(msg), -1)
		if len(matches) > 0 {
			return Muhafier(msg, "test", matches)
		}
		return ""
	}

	cases := []regCase{
		{"me.\nme", "muh muh"},
		{"\nme\nme", "muh muh"},
		{"me", "muh"},
		{"_me_", "_muh_"},
		{"__me__", "__muh__"},
		{"___me___", "___muh___"},
		{"*me*", "*muh*"},
		{"_me_", "_muh_"},
		{"**me**", "**muh**"},
		{"***me***", "***muh***"},
		{"***me***\"", "***muh***"},
		{"*__me__*\"", "__*muh*__"},
		{"~me~", "~muh~"},
		{"me.", "muh"},
		{"me?", "muh"},
		{"me!", "muh"},
		{"Me", "Muh"},
		{"mE", "muH"},
		{"ME", "MUH"},
		{"|me|", "|muh|"},
		{"||***~~ME~~***||", "||***~~MUH~~***||"},
		{"my name is paul", ""},
		{"game", "muh"},
		{"game.", "muh"},
		{"me(me)\nmuh.", "muh"},
		{"name asdasd", ""},
		{"none", ""},
	}

	for _, cs := range cases {
		result := testMuh(cs.str)
		var target string
		if cs.target == "" {
			target = ""
		} else {
			target = fmt.Sprintf("<@%s> %s ", "test", cs.target)
		}
		if result != target {
			t.Errorf("muh was incorrect was %s should have been %s", result, target)
		}
	}
}
