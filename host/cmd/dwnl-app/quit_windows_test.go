package main

import (
	"encoding/json"
	"testing"
)

func TestQuitTreatsOnlyUnfinishedStartedTasksAsADownloadInProgress(t *testing.T) {
	for _, tc := range []struct {
		states []string
		want   bool
	}{
		{nil, false},
		{[]string{"queued", "completed", "failed", "canceled", "interrupted"}, false},
		{[]string{"completed", "downloading"}, true},
		{[]string{"analyzing"}, true},
		{[]string{"canceling"}, true},
	} {
		tasks := []map[string]string{}
		for _, state := range tc.states {
			tasks = append(tasks, map[string]string{"id": "t", "state": state})
		}
		var decoded any
		data, _ := json.Marshal(map[string]any{"version": "0.3.0", "tasks": tasks})
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		got, err := activeTask(decoded)
		if err != nil || got != tc.want {
			t.Fatalf("%v: active=%v err=%v, want %v", tc.states, got, err, tc.want)
		}
	}
	if _, err := activeTask(map[string]any{"tasks": "not a list"}); err == nil {
		t.Fatal("a malformed snapshot was read")
	}
}

func TestQuitScopeIsDistinctFromTheInstanceLease(t *testing.T) {
	instance := instanceScope(`\\.\pipe\Dwnl.Desktop.example`)
	if instance == instanceScope(`\\.\pipe\Dwnl.Desktop.other`) || quitScope(instance) == instance {
		t.Fatalf("scopes collide: %q", instance)
	}
}
