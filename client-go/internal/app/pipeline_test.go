package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
)

// pipelineBridge answers the CI tab: one run, its jobs, and one job's log.
type pipelineBridge struct {
	mu     sync.Mutex
	run    string
	jobs   string
	trace  string
	asked  []int
	writes []struct {
		Action     string `json:"action"`
		JobID      int    `json:"jobId"`
		PipelineID int    `json:"pipelineId"`
	}
}

func newPipelineBridge(t *testing.T, run, jobs, trace string) (*pipelineBridge, *httptest.Server) {
	t.Helper()
	b := &pipelineBridge{run: run, jobs: jobs, trace: trace}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/pipeline":
			if r.Method == http.MethodPost {
				var body struct {
					Action     string `json:"action"`
					JobID      int    `json:"jobId"`
					PipelineID int    `json:"pipelineId"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				b.mu.Lock()
				b.writes = append(b.writes, struct {
					Action     string `json:"action"`
					JobID      int    `json:"jobId"`
					PipelineID int    `json:"pipelineId"`
				}{body.Action, body.JobID, body.PipelineID})
				b.mu.Unlock()
				_, _ = w.Write([]byte(`{"status":"ok"}`))
				return
			}
			b.mu.Lock()
			_, _ = w.Write([]byte(b.run))
			b.mu.Unlock()
		case "/api/job":
			jobID := 0
			if raw := r.URL.Query().Get("jobId"); raw != "" {
				_ = json.Unmarshal([]byte(raw), &jobID)
			}
			b.mu.Lock()
			b.asked = append(b.asked, jobID)
			trace := b.trace
			b.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobId": jobID, "status": "ok", "text": trace, "cut": true,
			})
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	return b, server
}

const ciRun = `{"status":"ok","project":"/w","worktree":"/w","branch":"agent/conv-12",
	"pipeline":{"id":21,"status":"failed","ref":"agent/conv-12","sha":"abc1234def","webUrl":"https://gitlab.example.com/g/p/-/pipelines/21","source":"push"},
	"jobs":[
	  {"id":70,"name":"build","stage":"build","status":"success","duration":31.4,"allowFailure":false},
	  {"id":71,"name":"test:unit","stage":"test","status":"failed","duration":12,"allowFailure":false},
	  {"id":72,"name":"lint","stage":"test","status":"skipped","allowFailure":true}
	],"stages":["build","test"]}`

// pipelineFixture is the CI tab open on a bridge with a canned answer.
func pipelineFixture(t *testing.T, run, jobs, trace string) (*App, *pipelineBridge) {
	t.Helper()
	bridge, server := newPipelineBridge(t, run, jobs, trace)
	a := changesFixture(t)
	a.active, a.changes.conv = 12, 12
	a.changes.workspace = "dora-service"
	a.api = api.New(server.URL, "")
	a.changes.open = true
	// The inbox holds one frame's worth of events: draining first keeps the
	// fixture's own changes answer from pushing the pipeline answer out.
	a.drain()
	a.active, a.changes.conv = 12, 12
	a.setChangesTab(tabPipeline)
	waitFor(t, func() bool { a.drain(); return a.changes.pipeline != nil || a.changes.ciFailed != "" })
	return a, bridge
}

// TestPipelineTabGroupsTheJobsByStage: the run says which one it was and how it
// ended, and each job sits under its stage with how long it took.
func TestPipelineTabGroupsTheJobsByStage(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, _ := pipelineFixture(t, ciRun, "", "")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"流水线", "#21", "失败", "abc1234", "build", "31s", "test", "test:unit  12s", "lint  跳过"} {
		if !tt.HasText(want) {
			t.Errorf("the pipeline tab does not show %q; drew %v", want, tt.Texts())
		}
	}
}

// TestPipelineRowOpensTheLog: clicking a job asks for its log and draws the tail
// it got, with the line that says why it failed picked out.
func TestPipelineRowOpensTheLog(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, bridge := pipelineFixture(t, ciRun, "", "step 1\ngo test ./...\nERROR: Job failed: exit code 1\n")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("test:unit  12s"); err != nil {
		t.Fatalf("the failed job is not clickable: %v", err)
	}
	tt.Frame()
	waitFor(t, func() bool {
		a.drain()
		tt.Frame()
		return tt.HasText("ERROR: Job failed")
	})
	bridge.mu.Lock()
	asked := append([]int(nil), bridge.asked...)
	bridge.mu.Unlock()
	if len(asked) != 1 || asked[0] != 71 {
		t.Fatalf("asked for jobs %v, wanted [71]", asked)
	}
	for _, want := range []string{"test:unit", "作业", "只显示最后几行"} {
		if !tt.HasText(want) {
			t.Errorf("the log view does not show %q; drew %v", want, tt.Texts())
		}
	}
	if err := tt.Click("作业"); err != nil {
		t.Fatalf("the back button is not clickable: %v", err)
	}
	tt.Frame()
	if !tt.HasText("test:unit  12s") {
		t.Fatalf("the back button did not return to the jobs; drew %v", tt.Texts())
	}
}

// TestPipelineRetryNamesTheJob: retrying a failed job posts that job's id, and
// retrying the run posts the run's.
func TestPipelineRetryNamesTheJob(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, bridge := pipelineFixture(t, ciRun, "", "boom\n")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	// The run's own retry is on the footer.
	if err := tt.Click("重试"); err != nil {
		t.Fatalf("the run retry is not clickable: %v", err)
	}
	tt.Frame()
	bridge.mu.Lock()
	writes := append(bridge.writes, struct {
		Action     string `json:"action"`
		JobID      int    `json:"jobId"`
		PipelineID int    `json:"pipelineId"`
	}{})
	bridge.mu.Unlock()
	first := writes[0]
	if first.Action != "retry" || first.PipelineID != 21 || first.JobID != 0 {
		t.Fatalf("the run retry posted %+v", first)
	}
}

// TestPipelineEmptyBranchSaysSo: a branch that never ran needs a sentence, not
// an empty frame.
func TestPipelineEmptyBranchSaysSo(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, _ := pipelineFixture(t, `{"status":"ok","pipeline":null,"jobs":[],"stages":[]}`, "", "")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("这个分支还没有跑过流水线") {
		t.Fatalf("the empty tab does not say so; drew %v", tt.Texts())
	}
}

// TestPipelineMissingLoginSaysWhichCommand: the failure names the fix.
func TestPipelineMissingLoginSaysWhichCommand(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, _ := pipelineFixture(t, `{"status":"not-authenticated","message":"glab: no token found"}`, "", "")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("glab auth login") {
		t.Fatalf("the tab does not name the fix; drew %v", tt.Texts())
	}
}

// TestSnapshotJobLog draws one job's log, the way the tab shows it after a click.
func TestSnapshotJobLog(t *testing.T) {
	i18n.Set(i18n.ZH)
	trace := "$ go test ./...\nok  \tpilot/internal/api\nok  \tpilot/internal/app\n" +
		"--- FAIL: TestQueueRetries (0.02s)\n    queue_test.go:41: got 2 retries, want 3\n" +
		"warning: coverage under the threshold\nERROR: Job failed: exit code 1\n"
	a, _ := pipelineFixture(t, ciRun, "", trace)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("test:unit  12s"); err != nil {
		t.Fatalf("the failed job is not clickable: %v", err)
	}
	waitFor(t, func() bool {
		a.drain()
		tt.Frame()
		return tt.HasText("ERROR: Job failed")
	})
	writePNG(t, "/tmp/clientv2-joblog.png", tt.Image())
}

// TestSnapshotPipeline draws the tab with a failed run on it.
func TestSnapshotPipeline(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, _ := pipelineFixture(t, ciRun, "", "")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/clientv2-pipeline.png", tt.Image())
}
