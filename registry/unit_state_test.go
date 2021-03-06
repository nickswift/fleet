// Copyright 2014 The fleet Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package registry

import (
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	etcd "github.com/coreos/etcd/client"
	"golang.org/x/net/context"

	"github.com/nickswift/fleet/machine"
	"github.com/nickswift/fleet/unit"
)

type action struct {
	key string
	val string
	rec bool
}

type testEtcdKeysAPI struct {
	etcd.KeysAPI
	gets    []action
	sets    []action
	deletes []action
	res     []*etcd.Response // errors returned from subsequent calls to etcd
	ri      int
	err     []error // results returned from subsequent calls to etcd
	ei      int
}

func (t *testEtcdKeysAPI) Set(_ context.Context, key string, value string, _ *etcd.SetOptions) (*etcd.Response, error) {
	t.sets = append(t.sets, action{key: key, val: value})
	return t.next()
}

func (t *testEtcdKeysAPI) Get(_ context.Context, key string, opts *etcd.GetOptions) (*etcd.Response, error) {
	act := action{key: key}
	if opts != nil {
		act.rec = opts.Recursive
	}
	t.gets = append(t.gets, act)
	return t.next()
}

func (t *testEtcdKeysAPI) Delete(_ context.Context, key string, opts *etcd.DeleteOptions) (*etcd.Response, error) {
	act := action{key: key}
	if opts != nil {
		act.rec = opts.Recursive
	}
	t.deletes = append(t.deletes, act)
	return t.next()
}

func (t *testEtcdKeysAPI) next() (r *etcd.Response, e error) {
	if t.ri < len(t.res) {
		r = t.res[t.ri]
		t.ri++
	}
	if t.ei < len(t.err) {
		e = t.err[t.ei]
		t.ei++
	}
	return r, e
}

func TestUnitStatePaths(t *testing.T) {
	r := &EtcdRegistry{kAPI: nil, keyPrefix: "/fleet/"}
	j := "foo.service"
	want := "/fleet/state/foo.service"
	got := r.legacyUnitStatePath(j)
	if got != want {
		t.Errorf("bad unit state path: got %v, want %v", got, want)
	}
	m := "abcdefghij"
	want = "/fleet/states/foo.service/abcdefghij"
	got = r.unitStatePath(m, j)
	if got != want {
		t.Errorf("bad unit state path: got %v, want %v", got, want)
	}
}

func TestSaveUnitState(t *testing.T) {
	e := &testEtcdKeysAPI{}
	r := &EtcdRegistry{kAPI: e, keyPrefix: "/fleet/"}
	j := "foo.service"
	mID := "mymachine"
	us := unit.NewUnitState("abc", "def", "ghi", mID)

	// Saving nil unit state should fail
	r.SaveUnitState(j, nil, time.Second)
	if e.sets != nil || e.deletes != nil {
		t.Logf("sets: %#v", e.sets)
		t.Logf("deletes: %#v", e.deletes)
		t.Fatalf("SaveUnitState of nil state should fail but acted unexpectedly!")
	}

	// Saving unit state with no hash should should fail.
	r.SaveUnitState(j, us, time.Second)
	if len(e.sets) != 0 || e.deletes != nil {
		t.Logf("sets: %#v", e.sets)
		t.Logf("deletes: %#v", e.deletes)
		t.Fatalf("SaveUnitState on UnitState with no hash acted unexpectedly!")
	}

	us.UnitHash = "quickbrownfox"
	r.SaveUnitState(j, us, time.Second)

	json := `{"loadState":"abc","activeState":"def","subState":"ghi","machineState":{"ID":"mymachine","PublicIP":"","Metadata":null,"Capabilities":null,"Version":""},"unitHash":"quickbrownfox"}`
	p1 := "/fleet/state/foo.service"
	p2 := "/fleet/states/foo.service/mymachine"
	want := []action{
		action{key: p1, val: json},
		action{key: p2, val: json},
	}
	got := e.sets
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bad result from SaveUnitState: \ngot\n%#v\nwant\n%#v", got, want)
	}
	if e.deletes != nil {
		t.Errorf("unexpected deletes during SaveUnitState: %#v", e.deletes)
	}
	if e.gets != nil {
		t.Errorf("unexpected gets during SaveUnitState: %#v", e.gets)
	}
}

func TestRemoveUnitState(t *testing.T) {
	e := &testEtcdKeysAPI{}
	r := &EtcdRegistry{kAPI: e, keyPrefix: "/fleet/"}
	j := "foo.service"
	err := r.RemoveUnitState(j)
	if err != nil {
		t.Errorf("unexpected error from RemoveUnitState: %v", err)
	}
	want := []action{
		action{key: "/fleet/state/foo.service", rec: false},
		action{key: "/fleet/states/foo.service", rec: true},
	}
	got := e.deletes
	if !reflect.DeepEqual(got, want) {
		t.Errorf("bad result from RemoveUnitState: \ngot\n%#v\nwant\n%#v", got, want)
	}
	if e.sets != nil {
		t.Errorf("unexpected sets during RemoveUnitState: %#v", e.sets)
	}
	if e.gets != nil {
		t.Errorf("unexpected gets during RemoveUnitState: %#v", e.gets)
	}

	// Ensure RemoveUnitState handles different error scenarios appropriately
	for i, tt := range []struct {
		errs []error
		fail bool
	}{
		{[]error{etcd.Error{Code: etcd.ErrorCodeKeyNotFound}}, false},
		{[]error{nil, etcd.Error{Code: etcd.ErrorCodeKeyNotFound}}, false},
		{[]error{nil, nil}, false}, // No errors, no responses should succeed
		{[]error{errors.New("ur registry don't work")}, true},
		{[]error{nil, errors.New("ur registry don't work")}, true},
	} {
		e = &testEtcdKeysAPI{err: tt.errs}
		r = &EtcdRegistry{kAPI: e, keyPrefix: "/fleet"}
		err = r.RemoveUnitState("foo.service")
		if (err != nil) != tt.fail {
			t.Errorf("case %d: unexpected error state calling UnitStates(): got %v, want %v", i, err, tt.fail)
		}
	}
}

func TestUnitStateToModel(t *testing.T) {
	for i, tt := range []struct {
		in   *unit.UnitState
		want *unitStateModel
	}{
		{
			in:   nil,
			want: nil,
		},
		{
			// Unit state with no hash and no machineID is not OK
			in: &unit.UnitState{
				LoadState:   "foo",
				ActiveState: "bar",
				SubState:    "baz",
				MachineID:   "",
				UnitHash:    "",
				UnitName:    "name",
			},
			want: nil,
		},
		{
			// Unit state with hash but no machineID is OK
			in: &unit.UnitState{
				LoadState:   "foo",
				ActiveState: "bar",
				SubState:    "baz",
				MachineID:   "",
				UnitHash:    "heh",
				UnitName:    "name",
			},
			want: &unitStateModel{
				LoadState:    "foo",
				ActiveState:  "bar",
				SubState:     "baz",
				MachineState: nil,
				UnitHash:     "heh",
			},
		},
		{
			in: &unit.UnitState{
				LoadState:   "foo",
				ActiveState: "bar",
				SubState:    "baz",
				MachineID:   "woof",
				UnitHash:    "miaow",
				UnitName:    "name",
			},
			want: &unitStateModel{
				LoadState:    "foo",
				ActiveState:  "bar",
				SubState:     "baz",
				MachineState: &machine.MachineState{ID: "woof"},
				UnitHash:     "miaow",
			},
		},
	} {
		got := unitStateToModel(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("case %d: got %#v, want %#v", i, got, tt.want)
		}
	}
}

func TestModelToUnitState(t *testing.T) {
	for i, tt := range []struct {
		in   *unitStateModel
		want *unit.UnitState
	}{
		{
			in:   nil,
			want: nil,
		},
		{
			in: &unitStateModel{"foo", "bar", "baz", nil, ""},
			want: &unit.UnitState{
				LoadState:   "foo",
				ActiveState: "bar",
				SubState:    "baz",
				MachineID:   "",
				UnitHash:    "",
				UnitName:    "name",
			},
		},
		{
			in: &unitStateModel{"z", "x", "y", &machine.MachineState{ID: "abcd"}, ""},
			want: &unit.UnitState{
				LoadState:   "z",
				ActiveState: "x",
				SubState:    "y",
				MachineID:   "abcd",
				UnitHash:    "",
				UnitName:    "name",
			},
		},
	} {
		got := modelToUnitState(tt.in, "name")
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("case %d: got %#v, want %#v", i, got, tt.want)
		}
	}
}

func makeResponse(val string) *etcd.Response {
	return &etcd.Response{
		Node: &etcd.Node{
			Value: val,
		},
	}
}

func TestGetUnitState(t *testing.T) {
	tests := []struct {
		res     *etcd.Response // result returned from etcd
		err     error          // error returned from etcd
		wantUS  *unit.UnitState
		wantErr bool
	}{
		{
			// Unit state with no UnitHash should be OK
			res: makeResponse(`{"loadState":"abc","activeState":"def","subState":"ghi","machineState":{"ID":"mymachine","PublicIP":"","Metadata":null,"Capabilities":null,"Version":"","TotalResources":{"Cores":0,"Memory":0,"Disk":0},"FreeResources":{"Cores":0,"Memory":0,"Disk":0}}}`),
			err: nil,
			wantUS: &unit.UnitState{
				LoadState:   "abc",
				ActiveState: "def",
				SubState:    "ghi",
				MachineID:   "mymachine",
				UnitHash:    "",
				UnitName:    "foo.service",
			},
		},
		{
			// Unit state with UnitHash should be OK
			res: makeResponse(`{"loadState":"abc","activeState":"def","subState":"ghi","machineState":{"ID":"mymachine","PublicIP":"","Metadata":null,"Capabilities":null,"Version":"","TotalResources":{"Cores":0,"Memory":0,"Disk":0},"FreeResources":{"Cores":0,"Memory":0,"Disk":0}},"unitHash":"quickbrownfox"}`),
			err: nil,
			wantUS: &unit.UnitState{
				LoadState:   "abc",
				ActiveState: "def",
				SubState:    "ghi",
				MachineID:   "mymachine",
				UnitHash:    "quickbrownfox",
				UnitName:    "foo.service",
			},
		},
		{
			// Unit state with no MachineState should be OK
			res: makeResponse(`{"loadState":"abc","activeState":"def","subState":"ghi"}`),
			err: nil,
			wantUS: &unit.UnitState{
				LoadState:   "abc",
				ActiveState: "def",
				SubState:    "ghi",
				MachineID:   "",
				UnitHash:    "",
				UnitName:    "foo.service",
			},
		},
		{
			// Bad unit state object should simply result in nil returned
			res:     makeResponse(`garbage, not good proper json`),
			err:     nil,
			wantUS:  nil,
			wantErr: true,
		},
		{
			// Unknown errors should result in nil returned
			res:     nil,
			err:     errors.New("some random error from etcd"),
			wantUS:  nil,
			wantErr: true,
		},
		{
			// KeyNotFound should result in nil returned
			res:     nil,
			err:     etcd.Error{Code: etcd.ErrorCodeKeyNotFound},
			wantUS:  nil,
			wantErr: false,
		},
	}

	for i, tt := range tests {
		e := &testEtcdKeysAPI{
			res: []*etcd.Response{tt.res},
			err: []error{tt.err},
		}
		r := &EtcdRegistry{kAPI: e, keyPrefix: "/fleet/"}
		j := "foo.service"
		us, err := r.getUnitState(j, "XXX")
		if tt.wantErr != (err != nil) {
			t.Errorf("case %d: unexpected error %t, got %v", i, tt.wantErr, err)
			continue
		}

		want := []action{
			action{key: "/fleet/states/foo.service/XXX", rec: false},
		}
		got := e.gets
		if !reflect.DeepEqual(got, want) {
			t.Errorf("case %d: bad result from GetUnitState:\ngot\n%#v\nwant\n%#v", i, got, want)
		}
		if !reflect.DeepEqual(us, tt.wantUS) {
			t.Errorf("case %d: bad UnitState:\ngot\n%#v\nwant\n%#v", i, us, tt.wantUS)
		}
	}
}

func usToJson(t *testing.T, us *unit.UnitState) string {
	json, err := marshal(unitStateToModel(us))
	if err != nil {
		t.Fatalf("error marshalling unit: %v", err)
	}
	return json
}

func TestUnitStates(t *testing.T) {
	fus1 := unit.UnitState{
		LoadState:   "abc",
		ActiveState: "def",
		SubState:    "ghi",
		MachineID:   "mID1",
		UnitHash:    "zzz",
		UnitName:    "foo",
	}
	fus2 := unit.UnitState{
		LoadState:   "cat",
		ActiveState: "dog",
		SubState:    "cow",
		MachineID:   "mID2",
		UnitHash:    "xxx",
		UnitName:    "foo",
	}
	// Multiple new unit states reported for the same unit
	foo := &etcd.Node{
		Key: "/fleet/states/foo",
		Nodes: []*etcd.Node{
			&etcd.Node{
				Key:   "/fleet/states/foo/mID1",
				Value: usToJson(t, &fus1),
			},
			&etcd.Node{
				Key:   "/fleet/states/foo/mID2",
				Value: usToJson(t, &fus2),
			},
		},
	}
	// Bogus new unit state which we won't expect to see in results
	bar := &etcd.Node{
		Key: "/fleet/states/bar",
		Nodes: []*etcd.Node{
			&etcd.Node{
				Key:   "/fleet/states/bar/asdf",
				Value: `total garbage`,
			},
		},
	}
	// Response from crawling the new "states" namespace
	res2 := &etcd.Response{
		Node: &etcd.Node{
			Key:   "/fleet/states",
			Nodes: []*etcd.Node{foo, bar},
		},
	}
	e := &testEtcdKeysAPI{
		res: []*etcd.Response{res2},
	}
	r := &EtcdRegistry{kAPI: e, keyPrefix: "/fleet/"}

	got, err := r.UnitStates()
	if err != nil {
		t.Errorf("unexpected error calling UnitStates(): %v", err)
	}

	want := []*unit.UnitState{
		&fus1,
		&fus2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UnitStates() returned unexpected result")
		t.Log("got:")
		for _, i := range got {
			t.Logf("%#v", i)
		}
		t.Log("want:")
		for _, i := range want {
			t.Logf("%#v", i)
		}
	}

	// Ensure UnitState handles different error scenarios appropriately
	for i, tt := range []struct {
		errs []error
		fail bool
	}{
		{[]error{etcd.Error{Code: etcd.ErrorCodeKeyNotFound}}, false},
		{[]error{etcd.Error{Code: etcd.ErrorCodeKeyNotFound}}, false},
		{[]error{nil}, false}, // No errors, no responses should succeed
		{[]error{errors.New("ur registry don't work")}, true},
		{[]error{errors.New("ur registry don't work")}, true},
	} {
		e = &testEtcdKeysAPI{err: tt.errs}
		r = &EtcdRegistry{kAPI: e, keyPrefix: "/fleet"}
		got, err = r.UnitStates()
		if (err != nil) != tt.fail {
			t.Errorf("case %d: unexpected error state calling UnitStates(): got %v, want %v", i, err, tt.fail)
		}
		if len(got) != 0 {
			t.Errorf("case %d: UnitStates() returned unexpected non-empty result on error: %v", i, got)
		}
	}
}

func TestMUSKeys(t *testing.T) {
	equal := func(a MUSKeys, b []MUSKey) bool {
		if len(a) != len(b) {
			return false
		}
		for i, m := range a {
			if m != b[i] {
				return false
			}
		}
		return true
	}
	k1 := MUSKey{Name: "abc", MachID: "aaa"}
	k2 := MUSKey{Name: "abc", MachID: "zzz"}
	k3 := MUSKey{Name: "def", MachID: "bbb"}
	k4 := MUSKey{Name: "ppp", MachID: "zzz"}
	k5 := MUSKey{Name: "xxx", MachID: "aaa"}
	want := []MUSKey{k1, k2, k3, k4, k5}
	ms := MUSKeys{k3, k4, k5, k2, k1}
	if equal(ms, want) {
		t.Fatalf("this should never happen!")
	}
	sort.Sort(ms)
	if !equal(ms, want) {
		t.Errorf("bad result after sort: got\n%#v, want\n%#v", ms, want)
	}
}
