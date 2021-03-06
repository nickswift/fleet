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

package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/nickswift/fleet/job"
	"github.com/nickswift/fleet/log"
)

var cmdStop = &cobra.Command{
	Use:   "stop [--no-block|--block-attempts=N] UNIT...",
	Short: "Instruct systemd to stop one or more units in the cluster.",
	Long: `Stop one or more units from running in the cluster, but allow them to be
started again in the future.

Instructs systemd on the host machine to stop the unit, deferring to systemd
completely for any custom stop directives (i.e. ExecStop option in the unit
file).

For units which are not global, stop operations are performed synchronously,
which means fleetctl will block until it detects that the unit(s) have
transitioned to a stopped state. This behaviour can be configured with the
respective --block-attempts and --no-block options. Stop operations on global
units are always non-blocking.

Stop a single unit:
fleetctl stop foo.service

Stop an entire directory of units with glob matching, without waiting:
fleetctl --no-block stop myservice/*`,
	Run: runWrapper(runStopUnit),
}

func init() {
	cmdFleet.AddCommand(cmdStop)

	cmdStop.Flags().IntVar(&sharedFlags.BlockAttempts, "block-attempts", 0, "Wait until the units are stopped, performing up to N attempts before giving up. A value of 0 indicates no limit. Does not apply to global units.")
	cmdStop.Flags().BoolVar(&sharedFlags.NoBlock, "no-block", false, "Do not wait until the units have stopped before exiting. Always the case for global units.")
}

func runStopUnit(cCmd *cobra.Command, args []string) (exit int) {
	if len(args) == 0 {
		stderr("No units given")
		return 0
	}

	units, err := findUnits(args)
	if err != nil {
		stderr("%v", err)
		return 1
	}

	if len(units) == 0 {
		stderr("Units not found in registry")
		return 0
	}

	stopping := make([]string, 0)
	for _, u := range units {
		if !suToGlobal(u) {
			if job.JobState(u.CurrentState) == job.JobStateInactive {
				stderr("Unable to stop unit %s in state %s", u.Name, job.JobStateInactive)
				return 1
			} else if job.JobState(u.CurrentState) == job.JobStateLoaded {
				log.Debugf("Unit(%s) already %s, skipping.", u.Name, job.JobStateLoaded)
				continue
			}
		}

		log.Debugf("Setting target state of Unit(%s) to %s", u.Name, job.JobStateLoaded)
		cAPI.SetUnitTargetState(u.Name, string(job.JobStateLoaded))
		if suToGlobal(u) {
			stdout("Triggered global unit %s stop", u.Name)
		} else {
			stopping = append(stopping, u.Name)
		}
	}

	err = tryWaitForUnitStates(stopping, "stop", job.JobStateLoaded, getBlockAttempts(cCmd), os.Stdout)
	if err != nil {
		stderr("Failed to stop units %v. err: %v", stopping, err)
		return 1
	}

	stderr("Successfully stopped units %v.", stopping)
	return 0
}
