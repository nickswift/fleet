package client

import (
	"reflect"
	"testing"

	"github.com/coreos/fleet/job"
	"github.com/coreos/fleet/machine"
	"github.com/coreos/fleet/schema"
	"github.com/coreos/fleet/unit"
)

func TestMapUnitEntityToJob(t *testing.T) {
	loaded := job.JobStateLoaded
	inactive := job.JobStateInactive

	tests := []struct {
		input  schema.Unit
		expect *job.Job
	}{
		{
			schema.Unit{
				Name:         "XXX",
				CurrentState: "loaded",
				DesiredState: "inactive",
				Systemd: &schema.SystemdState{
					LoadState:   "loaded",
					ActiveState: "active",
					SubState:    "running",
				},
				FileContents: "W1NlcnZpY2VdCkV4ZWNTdGFydD0vdXNyL2Jpbi9zbGVlcCAzMDAwCg==",
				FileHash:     "248b997d6becee1b835b7ec7d9c8e68d7dd24623",
			},
			&job.Job{
				Name:        "XXX",
				State:       &loaded,
				TargetState: inactive,
				Unit: unit.Unit{
					Raw: "[Service]\nExecStart=/usr/bin/sleep 3000\n",
					Contents: map[string]map[string][]string{
						"Service": map[string][]string{
							"ExecStart": []string{"/usr/bin/sleep 3000"},
						},
					},
				},
				UnitState: &unit.UnitState{
					UnitName:    "XXX",
					LoadState:   "loaded",
					ActiveState: "active",
					SubState:    "running",
				},
			},
		},

		// Lack of LoadState should result in a nil UnitState
		{
			schema.Unit{
				Name:         "XXX",
				CurrentState: "loaded",
				DesiredState: "loaded",
				FileContents: "W1NlcnZpY2VdCkV4ZWNTdGFydD0vdXNyL2Jpbi9zbGVlcCAzMDAwCg==",
				FileHash:     "248b997d6becee1b835b7ec7d9c8e68d7dd24623",
			},
			&job.Job{
				Name:        "XXX",
				State:       &loaded,
				TargetState: loaded,
				Unit: unit.Unit{
					Raw: "[Service]\nExecStart=/usr/bin/sleep 3000\n",
					Contents: map[string]map[string][]string{
						"Service": map[string][]string{
							"ExecStart": []string{"/usr/bin/sleep 3000"},
						},
					},
				},
			},
		},
	}

	for i, tt := range tests {
		output, err := mapUnitToJob(&tt.input, nil)
		if err != nil {
			t.Errorf("case %d: err=%v", i, err)
			continue
		}
		if !reflect.DeepEqual(tt.expect, output) {
			t.Errorf("case %d: expect=%v, got=%v", i, tt.expect, *output)
		}
	}
}

func TestMapUnitEntityToJobFailure(t *testing.T) {
	units := []schema.Unit{
		// Poorly-formatted FileContents should result in an error
		schema.Unit{
			Name:         "XXX",
			CurrentState: "loaded",
			Systemd: &schema.SystemdState{
				LoadState:   "loaded",
				ActiveState: "active",
				SubState:    "running",
				MachineID:   "YYY",
			},
			FileContents: "XXX",
			FileHash:     "248b997d6becee1b835b7ec7d9c8e68d7dd24623",
		},
	}

	for i, u := range units {
		output, err := mapUnitToJob(&u, nil)
		if err == nil {
			t.Errorf("case %d: expected non-nil error", i)
		}
		if output != nil {
			t.Errorf("case %d: expected nil Job, got %v", i, output)
		}
	}
}

func TestMapUnitEntityToJobMachineFields(t *testing.T) {
	tests := []struct {
		input  schema.Unit
		expect *job.Job
	}{
		{
			schema.Unit{
				Systemd: &schema.SystemdState{
					LoadState:   "loaded",
					ActiveState: "active",
					SubState:    "running",
					MachineID:   "YYY",
				},
			},
			&job.Job{
				UnitState: &unit.UnitState{
					LoadState:   "loaded",
					ActiveState: "active",
					SubState:    "running",
					MachineID:   "YYY",
				},
			},
		},

		// Missing MachineState in map does not result in loss of Machine ID
		{
			schema.Unit{
				Systemd: &schema.SystemdState{
					LoadState:   "loaded",
					ActiveState: "active",
					SubState:    "running",
					MachineID:   "FFF",
				},
			},
			&job.Job{
				UnitState: &unit.UnitState{
					LoadState:   "loaded",
					ActiveState: "active",
					SubState:    "running",
					MachineID:   "FFF",
				},
			},
		},
	}

	mm := map[string]*machine.MachineState{
		"YYY": &machine.MachineState{ID: "YYY", PublicIP: "ZZZ"},
	}

	for i, tt := range tests {
		output, err := mapUnitToJob(&tt.input, mm)
		if err != nil {
			t.Errorf("case %d: err=%v", i, err)
			continue
		}
		if !reflect.DeepEqual(tt.expect.UnitState, output.UnitState) {
			t.Errorf("case %d: expect=%v, got=%v", i, tt.expect.UnitState, output.UnitState)
		}
	}
}
