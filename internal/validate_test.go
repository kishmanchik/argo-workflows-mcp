package internal

import "testing"

func TestValidateNamespace(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"hxdr-dev1", false},
		{"a", false},
		{"kube-system", false},
		{"", true},
		{"UPPER", true},
		{"--all-namespaces", true}, // flag-injection attempt
		{"-n kube-system", true},
		{"has space", true},
		{"trailing-", true},
	}
	for _, c := range cases {
		err := ValidateNamespace(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateNamespace(%q): err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}

func TestValidateWorkflowName(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"my-workflow-abc123", false},
		{"--terminate", true}, // flag-injection attempt
		{"", true},
		{"my workflow", true},
	}
	for _, c := range cases {
		err := ValidateWorkflowName(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateWorkflowName(%q): err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}

func TestValidatePhase(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"", false},
		{"Running", false},
		{"Failed", false},
		{"Succeeded", false},
		{"Bogus", true},
		{"--all", true},
	}
	for _, c := range cases {
		err := ValidatePhase(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidatePhase(%q): err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}

func TestValidateTail(t *testing.T) {
	if got, err := ValidateTail(0); err != nil || got != 50 {
		t.Errorf("ValidateTail(0) = %d, %v; want 50, nil (default)", got, err)
	}
	if got, err := ValidateTail(-5); err != nil || got != 50 {
		t.Errorf("ValidateTail(-5) = %d, %v; want 50, nil (default)", got, err)
	}
	if got, err := ValidateTail(100); err != nil || got != 100 {
		t.Errorf("ValidateTail(100) = %d, %v; want 100, nil", got, err)
	}
	if _, err := ValidateTail(501); err == nil {
		t.Error("ValidateTail(501) = nil error; want error (exceeds max)")
	}
}
