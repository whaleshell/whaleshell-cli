package cli

import (
	"strings"
	"testing"
)

func TestHelpNested(t *testing.T) {
	root := helpText(nil)
	if root == "" || !strings.Contains(root, "osg") {
		t.Fatal("root help empty")
	}
	sb := helpText([]string{"sandbox"})
	if !strings.Contains(sb, "sandbox create") {
		t.Fatalf("sandbox help: %s", sb)
	}
	create := helpText([]string{"sandbox", "create"})
	if !strings.Contains(create, "--name") {
		t.Fatalf("create help: %s", create)
	}
	if !wantsHelp([]string{"sandbox", "--help"}) {
		t.Fatal("wantsHelp")
	}
	path := helpPath([]string{"service", "expose", "-h"})
	if len(path) != 2 || path[0] != "service" {
		t.Fatalf("helpPath %#v", path)
	}
}
