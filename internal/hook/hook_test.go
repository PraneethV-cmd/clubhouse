package hook

import (
	"bytes"
	"io"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"clubhouse/internal/client"
	"clubhouse/internal/server"
)

func TestFilesFromApplyPatch(t *testing.T) {
	var in input
	in.Cwd = "/repo"
	in.ToolInput.Command = "*** Begin Patch\n*** Update File: src/auth.ts\n@@\n+// x\n*** Add File: src/new.ts\n*** End Patch\n"
	got := in.files()
	want := []string{"src/auth.ts", "src/new.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("apply_patch files = %v, want %v", got, want)
	}
}

func TestFilesFromFilePath(t *testing.T) {
	var in input
	in.Cwd = "/repo"
	in.ToolInput.FilePath = "/repo/src/billing.ts" // absolute -> repo-relative
	got := in.files()
	if !reflect.DeepEqual(got, []string{"src/billing.ts"}) {
		t.Fatalf("file_path files = %v", got)
	}
}

func TestFilesSkipsOutsideRepo(t *testing.T) {
	var in input
	in.Cwd = "/repo"
	in.ToolInput.FilePath = "/tmp/outside.txt"
	got := in.files()
	if len(got) != 0 {
		t.Fatalf("outside file should not be locked: %v", got)
	}
}

func TestFilesCleansRelativePaths(t *testing.T) {
	var in input
	in.Cwd = "/repo"
	in.ToolInput.FilePath = "src/../src/auth.ts"
	got := in.files()
	if !reflect.DeepEqual(got, []string{"src/auth.ts"}) {
		t.Fatalf("cleaned files = %v", got)
	}
}

func TestAllowIsSilent(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	allow()
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "" {
		t.Fatalf("allow emitted %q", got)
	}
}

func TestClaimFilesRollsBackEarlierLocksOnConflict(t *testing.T) {
	s := server.New("test", "roomtok", "", "http://box:8787")
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	ada := client.New(ts.URL, "roomtok")
	if _, err := ada.Join("ada"); err != nil {
		t.Fatal(err)
	}
	bo := client.New(ts.URL, "roomtok")
	if _, err := bo.Join("bo"); err != nil {
		t.Fatal(err)
	}
	if _, err := bo.Lock("b.txt", "editing"); err != nil {
		t.Fatal(err)
	}

	reason, blocked := claimFiles(ada, []string{"a.txt", "b.txt"})
	if !blocked || reason == "" {
		t.Fatalf("expected conflict reason, got blocked=%v reason=%q", blocked, reason)
	}
	r, err := ada.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Locks["a.txt"]; ok {
		t.Fatalf("a.txt should have been released after b.txt conflict: %#v", r.Locks)
	}
	if _, ok := r.Locks["b.txt"]; !ok {
		t.Fatalf("b.txt should still be held by the original member: %#v", r.Locks)
	}
}
