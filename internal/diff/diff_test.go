package diff

import (
	"fmt"
	"strings"
	"testing"
)

// twoHunkPatch is a git diff of one file with two hunks, from twill.
const twoHunkPatch = "diff --git a/f b/f\n" +
	"index 111..222 100644\n" +
	"--- a/f\n" +
	"+++ b/f\n" +
	"@@ -5,3 +5,3 @@\n" +
	" ctx\n" +
	"-old\n" +
	"+new\n" +
	" ctx\n" +
	"@@ -20,3 +20,3 @@\n" +
	" ctx\n" +
	"-a\n" +
	"+b\n" +
	" ctx\n"

const newFilePatch = "diff --git a/new.txt b/new.txt\n" +
	"new file mode 100644\n" +
	"index 000..111\n" +
	"--- /dev/null\n" +
	"+++ b/new.txt\n" +
	"@@ -0,0 +1,2 @@\n" +
	"+hello\n" +
	"+world\n"

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		patch string
		want  string
	}{
		{
			name:  "git diff with two hunks",
			patch: twoHunkPatch,
			want: "file \"f\" modified +2-2\n" +
				"hunk @@ -5,3 +5,3 @@\n" +
				"5/5 context \" ctx\"\n" +
				"6/0 del \"-old\"\n" +
				"0/6 add \"+new\"\n" +
				"7/7 context \" ctx\"\n" +
				"hunk @@ -20,3 +20,3 @@\n" +
				"20/20 context \" ctx\"\n" +
				"21/0 del \"-a\"\n" +
				"0/21 add \"+b\"\n" +
				"22/22 context \" ctx\"\n",
		},
		{
			// A plain unified patch and hunks that only insert.
			name: "bare patch, pure insertion",
			patch: "--- a/f\n" +
				"+++ b/f\n" +
				"@@ -5,0 +6,2 @@\n" +
				"+one\n" +
				"+two\n" +
				"@@ -10,3 +12,3 @@\n" +
				" ctx\n" +
				"-x\n" +
				"+y\n" +
				" ctx\n",
			want: "file \"f\" modified +3-1\n" +
				"hunk @@ -5,0 +6,2 @@\n" +
				"0/6 add \"+one\"\n" +
				"0/7 add \"+two\"\n" +
				"hunk @@ -10,3 +12,3 @@\n" +
				"10/12 context \" ctx\"\n" +
				"11/0 del \"-x\"\n" +
				"0/13 add \"+y\"\n" +
				"12/14 context \" ctx\"\n",
		},
		{
			name:  "git new file",
			patch: newFilePatch,
			want: "file \"new.txt\" added +2-0\n" +
				"meta new file mode 100644\n" +
				"hunk @@ -0,0 +1,2 @@\n" +
				"0/1 add \"+hello\"\n" +
				"0/2 add \"+world\"\n",
		},
		{
			name: "git deleted file",
			patch: "diff --git a/gone.txt b/gone.txt\n" +
				"deleted file mode 100644\n" +
				"--- a/gone.txt\n" +
				"+++ /dev/null\n" +
				"@@ -1,1 +0,0 @@\n" +
				"-bye\n",
			want: "file \"gone.txt\" deleted +0-1\n" +
				"meta deleted file mode 100644\n" +
				"hunk @@ -1,1 +0,0 @@\n" +
				"1/0 del \"-bye\"\n",
		},
		{
			name: "git rename with an edit",
			patch: "diff --git a/old.go b/new.go\n" +
				"similarity index 90%\n" +
				"rename from old.go\n" +
				"rename to new.go\n" +
				"--- a/old.go\n" +
				"+++ b/new.go\n" +
				"@@ -1,2 +1,2 @@\n" +
				" package x\n" +
				"-var a = 1\n" +
				"+var a = 2\n",
			want: "file \"new.go\" renamed +1-1\n" +
				"meta similarity index 90%\n" +
				"meta rename from old.go\n" +
				"meta rename to new.go\n" +
				"hunk @@ -1,2 +1,2 @@\n" +
				"1/1 context \" package x\"\n" +
				"2/0 del \"-var a = 1\"\n" +
				"0/2 add \"+var a = 2\"\n",
		},
		{
			name: "binary file",
			patch: "diff --git a/logo.png b/logo.png\n" +
				"index 111..222 100644\n" +
				"Binary files a/logo.png and b/logo.png differ\n",
			want: "file \"logo.png\" modified +0-0\n" +
				"meta Binary files a/logo.png and b/logo.png differ\n",
		},
		{
			name: "mode change only",
			patch: "diff --git a/run.sh b/run.sh\n" +
				"old mode 100644\n" +
				"new mode 100755\n",
			want: "file \"run.sh\" modified +0-0\n" +
				"meta old mode 100644\n" +
				"meta new mode 100755\n",
		},
		{
			// What a tool prints when it shows only the changed hunks.
			name: "hunk only fragment",
			patch: "@@ -10,2 +10,2 @@ func main() {\n" +
				" \tfmt.Println(1)\n" +
				"-\tfmt.Println(2)\n" +
				"+\tfmt.Println(3)\n",
			want: "file \"\" modified +1-1\n" +
				"hunk @@ -10,2 +10,2 @@ func main() {\n" +
				"10/10 context \" \\tfmt.Println(1)\"\n" +
				"11/0 del \"-\\tfmt.Println(2)\"\n" +
				"0/11 add \"+\\tfmt.Println(3)\"\n",
		},
		{
			name:  "empty patch",
			patch: "",
			want:  "",
		},
		{
			name:  "meta only patch",
			patch: "Binary files a/x and b/x differ\n",
			want: "file \"\" modified +0-0\n" +
				"meta Binary files a/x and b/x differ\n",
		},
		{
			// The hunk counts say where the hunk ends, so content that looks
			// like a file header stays content.
			name: "header shaped lines inside a hunk",
			patch: "--- a/f\n" +
				"+++ b/f\n" +
				"@@ -1,2 +1,2 @@\n" +
				"---\n" +
				"+++\n" +
				" ctx\n",
			want: "file \"f\" modified +1-1\n" +
				"hunk @@ -1,2 +1,2 @@\n" +
				"1/0 del \"---\"\n" +
				"0/1 add \"+++\"\n" +
				"2/2 context \" ctx\"\n",
		},
		{
			name: "missing newline markers",
			patch: "--- a/f\n" +
				"+++ b/f\n" +
				"@@ -1 +1 @@\n" +
				"-a\n" +
				"\\ No newline at end of file\n" +
				"+b\n" +
				"\\ No newline at end of file\n",
			want: "file \"f\" modified +1-1\n" +
				"hunk @@ -1 +1 @@\n" +
				"1/0 del \"-a\"\n" +
				"0/0 meta \"\\\\ No newline at end of file\"\n" +
				"0/1 add \"+b\"\n" +
				"0/0 meta \"\\\\ No newline at end of file\"\n",
		},
		{
			name: "two files in one patch",
			patch: twoHunkPatch +
				"diff --git a/second b/second\n" +
				"--- a/second\n" +
				"+++ b/second\n" +
				"@@ -1,1 +1,1 @@\n" +
				"-a\n" +
				"+b\n",
			want: "file \"f\" modified +2-2\n" +
				"hunk @@ -5,3 +5,3 @@\n" +
				"5/5 context \" ctx\"\n" +
				"6/0 del \"-old\"\n" +
				"0/6 add \"+new\"\n" +
				"7/7 context \" ctx\"\n" +
				"hunk @@ -20,3 +20,3 @@\n" +
				"20/20 context \" ctx\"\n" +
				"21/0 del \"-a\"\n" +
				"0/21 add \"+b\"\n" +
				"22/22 context \" ctx\"\n" +
				"file \"second\" modified +1-1\n" +
				"hunk @@ -1,1 +1,1 @@\n" +
				"1/0 del \"-a\"\n" +
				"0/1 add \"+b\"\n",
		},
		{
			name: "carriage returns",
			patch: "--- a/f\r\n" +
				"+++ b/f\r\n" +
				"@@ -1 +1 @@\r\n" +
				"-a\r\n" +
				"+b\r\n",
			want: "file \"f\" modified +1-1\n" +
				"hunk @@ -1 +1 @@\n" +
				"1/0 del \"-a\"\n" +
				"0/1 add \"+b\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dumpFiles(Parse(tt.patch)); got != tt.want {
				t.Errorf("Parse:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestStats(t *testing.T) {
	added, removed, paths := Stats(Parse(twoHunkPatch + newFilePatch))
	if added != 4 || removed != 2 || paths != 2 {
		t.Errorf("Stats = %d added, %d removed, %d paths; want 4, 2, 2", added, removed, paths)
	}
}

func dumpFiles(files []File) string {
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "file %q %s +%d-%d\n", f.Path, f.Status, f.Added, f.Removed)
		for _, m := range f.Meta {
			fmt.Fprintf(&b, "meta %s\n", m)
		}
		for _, h := range f.Hunks {
			fmt.Fprintf(&b, "hunk %s\n", h.Header)
			for _, l := range h.Lines {
				fmt.Fprintf(&b, "%d/%d %s %q\n", l.OldNo, l.NewNo, kindName(l.Kind), l.Text)
			}
		}
	}
	return b.String()
}

func kindName(k Kind) string {
	switch k {
	case Context:
		return "context"
	case Add:
		return "add"
	case Del:
		return "del"
	case Meta:
		return "meta"
	}
	return "unknown"
}
