package api

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// Each case is (baseline tree, op batch, the full value it must produce).
// Comparing the applied tree with a freshly decoded full value is the point:
// a delta that drifts by one index still parses, and only deep-equal catches it.
func TestApplyOps(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ops  []string
		want string
	}{
		{
			name: "replace the root",
			in:   `{"a":1}`,
			ops:  []string{`["r",{"b":[1,2],"c":null}]`},
			want: `{"b":[1,2],"c":null}`,
		},
		{
			name: "root replacement mid-stream",
			in:   `{"entries":[1,2,3]}`,
			ops:  []string{`["s",["entries"],[]]`, `["r",{"entries":[9]}]`},
			want: `{"entries":[9]}`,
		},
		{
			name: "set a new and an existing object key",
			in:   `{"a":1,"b":{"c":2}}`,
			ops:  []string{`["s",["a"],3]`, `["s",["d"],4]`, `["s",["b","c"],5]`},
			want: `{"a":3,"b":{"c":5},"d":4}`,
		},
		{
			name: "set an array element and append at the end",
			in:   `{"a":[1,2]}`,
			ops:  []string{`["s",["a",0],7]`, `["s",["a",2],3]`},
			want: `{"a":[7,2,3]}`,
		},
		{
			name: "delete an object key and a missing key",
			in:   `{"a":1,"b":2}`,
			ops:  []string{`["d",["a"]]`, `["d",["nope"]]`},
			want: `{"b":2}`,
		},
		{
			name: "delete an array element",
			in:   `{"a":[1,2,3]}`,
			ops:  []string{`["d",["a",1]]`},
			want: `{"a":[1,3]}`,
		},
		{
			name: "append to a string, folded twice",
			in:   `{"text":"he"}`,
			ops:  []string{`["a",["text"],"ll"]`, `["a",["text"],"o"]`},
			want: `{"text":"hello"}`,
		},
		{
			name: "truncate a string from an index",
			in:   `{"text":"hello"}`,
			ops:  []string{`["t",["text"],2]`},
			want: `{"text":"llo"}`,
		},
		{
			name: "truncate past the end yields an empty string",
			in:   `{"text":"hello"}`,
			ops:  []string{`["t",["text"],99]`},
			want: `{"text":""}`,
		},
		{
			name: "splice: insert in the middle",
			in:   `{"a":[1,4]}`,
			ops:  []string{`["p",["a"],1,0,[2,3]]`},
			want: `{"a":[1,2,3,4]}`,
		},
		{
			name: "splice: delete then insert",
			in:   `{"a":[1,2,3]}`,
			ops:  []string{`["p",["a"],1,1,[]]`, `["p",["a"],1,0,[9,8]]`},
			want: `{"a":[1,9,8,3]}`,
		},
		{
			name: "splice: start past the end clamps",
			in:   `{"a":[1,2]}`,
			ops:  []string{`["p",["a"],9,3,[7]]`},
			want: `{"a":[1,2,7]}`,
		},
		{
			name: "splice: the root itself is an array",
			in:   `[1,2]`,
			ops:  []string{`["p",[],1,1,[5,6]]`},
			want: `[1,5,6]`,
		},
		{
			name: "permute an array",
			in:   `{"a":["x","y","z"]}`,
			ops:  []string{`["m",["a"],[2,0,1]]`},
			want: `{"a":["z","x","y"]}`,
		},
		{
			name: "permute the root array",
			in:   `[1,2]`,
			ops:  []string{`["m",[],[1,0]]`},
			want: `[2,1]`,
		},
		{
			name: "deep nested text path",
			in:   `{"docs":{"pi.live":{"generation":{"message":{"content":[{"type":"text","text":"a"}]}}}}}`,
			ops:  []string{`["a",["docs","pi.live","generation","message","content",0,"text"],"b"]`},
			want: `{"docs":{"pi.live":{"generation":{"message":{"content":[{"type":"text","text":"ab"}]}}}}}`,
		},
		{
			name: "a batch is just its ops in order",
			in:   `{"entries":[{"id":1}],"docs":{"pi.live":{"busy":false,"tools":[]}}}`,
			ops: []string{
				`["p",["entries"],1,0,[{"id":2}]]`,
				`["s",["docs","pi.live","busy"],true]`,
				`["p",["docs","pi.live","tools"],0,0,[{"name":"bash"}]]`,
				`["s",["docs","pi.live","tools",0,"state"],"running"]`,
			},
			want: `{"entries":[{"id":1},{"id":2}],"docs":{"pi.live":{"busy":true,"tools":[{"name":"bash","state":"running"}]}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := decodeTree(json.RawMessage(tt.in))
			if err != nil {
				t.Fatalf("decode baseline: %v", err)
			}
			got, err := applyOps(root, rawOps(tt.ops))
			if err != nil {
				t.Fatalf("applyOps: %v", err)
			}
			want, err := decodeTree(json.RawMessage(tt.want))
			if err != nil {
				t.Fatalf("decode want: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("tree = %#v, want %#v", got, want)
			}
		})
	}
}

func TestApplyOpsRejectsMalformed(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ops  []string
	}{
		{"unknown verb", `{"a":1}`, []string{`["z",["a"],1]`}},
		{"path dictionary op", `{"a":1}`, []string{`["#",0,["a"]]`}},
		{"reserved segment", `{"a":1}`, []string{`["s",["__proto__"],1]`}},
		{"key into an array", `{"a":[1]}`, []string{`["s",["a","x"],1]`}},
		{"index into an object", `{"a":1}`, []string{`["s",[0],1]`}},
		{"path through a scalar", `{"a":1}`, []string{`["s",["a","b"],1]`}},
		{"missing intermediate key", `{"a":{"b":1}}`, []string{`["s",["a","c","d"],1]`}},
		{"array index past the end", `{"a":[1]}`, []string{`["s",["a",2],1]`}},
		{"append to a non-string", `{"a":1}`, []string{`["a",["a"],"x"]`}},
		{"truncate a non-string", `{"a":1}`, []string{`["t",["a"],1]`}},
		{"truncate a missing key", `{"a":1}`, []string{`["t",["b"],1]`}},
		{"splice a non-array", `{"a":1}`, []string{`["p",["a"],0,0,[]]`}},
		{"splice a missing path", `{"a":[1]}`, []string{`["p",["b"],0,0,[]]`}},
		{"permutation length", `{"a":[1,2]}`, []string{`["m",["a"],[0]]`}},
		{"permutation duplicate", `{"a":[1,2]}`, []string{`["m",["a"],[0,0]]`}},
		{"permutation out of range", `{"a":[1,2]}`, []string{`["m",["a"],[0,2]]`}},
		{"delete past the end", `{"a":[1]}`, []string{`["d",["a",3]]`}},
		{"ops is not a tuple", `{"a":1}`, []string{`"s"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := decodeTree(json.RawMessage(tt.in))
			if err != nil {
				t.Fatalf("decode baseline: %v", err)
			}
			if _, err := applyOps(root, rawOps(tt.ops)); err == nil {
				t.Fatal("applyOps succeeded, want an error")
			}
		})
	}
}

// Splitting one batch in two must land on the same tree: the bridge folds
// commits, so a batch boundary is not part of the semantics.
func TestApplyOpsBatchBoundaries(t *testing.T) {
	in := `{"a":[1,2],"text":"x","flag":false}`
	ops := []string{`["p",["a"],1,1,[7]]`, `["a",["text"],"yz"]`, `["s",["flag"],true]`}

	single, err := decodeTree(json.RawMessage(in))
	if err != nil {
		t.Fatal(err)
	}
	single, err = applyOps(single, rawOps(ops))
	if err != nil {
		t.Fatalf("single batch: %v", err)
	}

	split, err := decodeTree(json.RawMessage(in))
	if err != nil {
		t.Fatal(err)
	}
	for i, op := range ops {
		split, err = applyOps(split, rawOps([]string{op}))
		if err != nil {
			t.Fatalf("batch %d: %v", i, err)
		}
	}
	if !reflect.DeepEqual(single, split) {
		t.Fatalf("one batch = %#v, split batches = %#v", single, split)
	}
}

// Fixed seed: generated splices and permutations on a top-level array, checked
// against the array the test itself evolves. This is the p/m counterpart of the
// table above — the ops are computed independently of the applier.
func TestGeneratedArrayOps(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 10; trial++ {
		var current []int
		for i := 0; i < rng.Intn(4)+1; i++ {
			current = append(current, i)
		}
		baseline, err := json.Marshal(current)
		if err != nil {
			t.Fatal(err)
		}
		root, err := decodeTree(baseline)
		if err != nil {
			t.Fatal(err)
		}

		var ops []string
		for step := 0; step < 6; step++ {
			switch rng.Intn(3) {
			case 0: // insert
				at := rng.Intn(len(current) + 1)
				value := 100 + step
				current = insertAt(current, at, value)
				ops = append(ops, fmt.Sprintf(`["p",[],%d,0,[%d]]`, at, value))
			case 1: // delete
				if len(current) == 0 {
					continue
				}
				at := rng.Intn(len(current))
				current = append(current[:at:at], current[at+1:]...)
				ops = append(ops, fmt.Sprintf(`["p",[],%d,1,[]]`, at))
			case 2: // reorder
				perm := rng.Perm(len(current))
				next := make([]int, len(current))
				for i, from := range perm {
					next[i] = current[from]
				}
				current = next
				encoded, err := json.Marshal(perm)
				if err != nil {
					t.Fatal(err)
				}
				ops = append(ops, fmt.Sprintf(`["m",[],%s]`, encoded))
			}
		}

		got, err := applyOps(root, rawOps(ops))
		if err != nil {
			t.Fatalf("trial %d: applyOps: %v (ops %v)", trial, err, ops)
		}
		wantJSON, err := json.Marshal(current)
		if err != nil {
			t.Fatal(err)
		}
		want, err := decodeTree(wantJSON)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("trial %d: tree = %#v, want %#v (ops %v)", trial, got, want, ops)
		}
	}
}

func TestProject(t *testing.T) {
	root, err := decodeTree(json.RawMessage(`{
		"entries":[{"id":9223372036854775807,"kind":"pi.user","model":null,"data":{"text":"hi","big":12345678901234567890}}],
		"docs":{
			"pi.live":{"run":{"status":"running"},"generation":null,"tools":[]},
			"pi.inbox":{"items":[{"id":1,"mode":"steer"}]},
			"pi.usage":{"cost":1}
		},
		"meta":{"title":"t"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	value, err := project(root)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if len(value.Entries) != 1 || value.Entries[0].ID != 9223372036854775807 {
		t.Fatalf("entries = %+v", value.Entries)
	}
	// The raw JSON must keep the integer exactly: decoding through float64
	// would have rounded it.
	if got := string(value.Entries[0].Data); got != `{"big":12345678901234567890,"text":"hi"}` {
		t.Fatalf("entry data = %s", got)
	}
	if _, ok := value.Docs["pi.live"]; !ok {
		t.Fatal("pi.live missing")
	}
	if _, ok := value.Docs["pi.inbox"]; ok {
		t.Fatal("pi.inbox must not be projected: it would change composer behaviour")
	}

	// A view without pi.live is still a frame; the old decoder behaved this way.
	bare, err := decodeTree(json.RawMessage(`{"entries":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	bareValue, err := project(bare)
	if err != nil {
		t.Fatalf("project without docs: %v", err)
	}
	if len(bareValue.Docs) != 0 {
		t.Fatalf("docs = %+v, want empty", bareValue.Docs)
	}
}

func TestProjectRejectsBrokenView(t *testing.T) {
	for _, in := range []string{
		`[]`,
		`{"entries":{},"docs":{"pi.live":{}}}`,
		`{"docs":{"pi.live":{}}}`,
		`{"entries":[],"docs":[]}`,
		`{"entries":[],"docs":{"pi.live":null}}`,
	} {
		root, err := decodeTree(json.RawMessage(in))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := project(root); err == nil {
			t.Fatalf("project(%s) succeeded, want an error", in)
		}
	}
}

func rawOps(ops []string) []json.RawMessage {
	raw := make([]json.RawMessage, len(ops))
	for i, op := range ops {
		raw[i] = json.RawMessage(op)
	}
	return raw
}

func insertAt(values []int, at, value int) []int {
	out := make([]int, 0, len(values)+1)
	out = append(out, values[:at]...)
	out = append(out, value)
	out = append(out, values[at:]...)
	return out
}
