package types

import "testing"

// TestSaveRuntimeTaskPeriodicReqValidateNormalizesTargets 验证对应场景符合预期。
func TestSaveRuntimeTaskPeriodicReqValidateNormalizesTargets(t *testing.T) {
	req := &SaveRuntimeTaskPeriodicReq{
		Name:         "daily",
		Workflow:     "user_tag.refresh",
		EverySeconds: 60,
		Targets:      []string{" uid:1 ", "", "uid:1", " uid:2 "},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	want := []string{"uid:1", "uid:2"}
	if len(req.Targets) != len(want) {
		t.Fatalf("Targets = %#v, want %#v", req.Targets, want)
	}
	for i := range want {
		if req.Targets[i] != want[i] {
			t.Fatalf("Targets = %#v, want %#v", req.Targets, want)
		}
	}
}

// TestSaveRuntimeTaskPeriodicReqValidateKeepsEmptyTargetsNil 验证对应场景符合预期。
func TestSaveRuntimeTaskPeriodicReqValidateKeepsEmptyTargetsNil(t *testing.T) {
	req := &SaveRuntimeTaskPeriodicReq{
		Name:         "daily",
		Workflow:     "user_tag.refresh",
		EverySeconds: 60,
		Targets:      []string{" ", ""},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if req.Targets != nil {
		t.Fatalf("Targets = %#v, want nil", req.Targets)
	}
}
