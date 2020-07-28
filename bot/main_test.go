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
		regCase{"me.\nme", "muh muh"},
		regCase{"\nme\nme", "muh muh"},
		regCase{"me", "muh"},
		regCase{"_me_", "_muh_"},
		regCase{"__me__", "__muh__"},
		regCase{"___me___", "___muh___"},
		regCase{"*me*", "*muh*"},
		regCase{"_me_", "_muh_"},
		regCase{"**me**", "**muh**"},
		regCase{"***me***", "***muh***"},
		regCase{"***me***\"", "***muh***"},
		regCase{"*__me__*\"", "__*muh*__"},
		regCase{"~me~", "~muh~"},
		regCase{"me.", "muh"},
		regCase{"me?", "muh"},
		regCase{"me!", "muh"},
		regCase{"Me", "Muh"},
		regCase{"mE", "muH"},
		regCase{"ME", "MUH"},
		regCase{"my name is paul", ""},
		regCase{"game", "muh"},
		regCase{"game.", "muh"},
		regCase{"me(me)\nmuh.", "muh"},
		regCase{"name asdasd", ""},
		regCase{"none", ""},
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
