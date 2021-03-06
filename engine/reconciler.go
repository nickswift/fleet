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

package engine

import (
	"fmt"
	"time"

	"github.com/nickswift/fleet/job"
	"github.com/nickswift/fleet/log"
	"github.com/nickswift/fleet/metrics"
)

const (
	taskTypeUnscheduleUnit      = "UnscheduleUnit"
	taskTypeAttemptScheduleUnit = "AttemptScheduleUnit"
)

type task struct {
	Type      string
	Reason    string
	JobName   string
	MachineID string
}

func (t *task) String() string {
	return fmt.Sprintf("{Type: %s, JobName: %s, MachineID: %s, Reason: %q}", t.Type, t.JobName, t.MachineID, t.Reason)
}

func NewReconciler() *Reconciler {
	return &Reconciler{
		sched: &leastLoadedScheduler{},
	}
}

type Reconciler struct {
	sched Scheduler
}

func (r *Reconciler) Reconcile(e *Engine, stop chan struct{}) {
	log.Debugf("Polling Registry for actionable work")

	start := time.Now()

	clust, err := e.clusterState()
	if err != nil {
		log.Errorf("Failed getting current cluster state: %v", err)
		return
	}

	for t := range r.calculateClusterTasks(clust, stop) {
		err = doTask(t, e)
		if err != nil {
			log.Errorf("Failed resolving task: task=%s err=%v", t, err)
		}
	}

	metrics.ReportEngineReconcileSuccess(start)
}

func (r *Reconciler) calculateClusterTasks(clust *clusterState, stopchan chan struct{}) (taskchan chan *task) {
	taskchan = make(chan *task)

	send := func(typ, reason, jName, machID string) bool {
		select {
		case <-stopchan:
			return false
		default:
		}

		taskchan <- &task{Type: typ, Reason: reason, JobName: jName, MachineID: machID}
		return true
	}

	go func() {
		defer close(taskchan)

		agents := clust.agents()

		for _, j := range clust.jobs {
			if !j.Scheduled() {
				continue
			}

			decide := func() (unschedule bool, reason string) {
				if j.TargetState == job.JobStateInactive {
					unschedule = true
					reason = "target state inactive"
					return
				}

				as, ok := agents[j.TargetMachineID]
				if !ok {
					unschedule = true
					reason = fmt.Sprintf("target Machine(%s) went away", j.TargetMachineID)
					metrics.ReportEngineReconcileFailure(metrics.MachineAway)
					return
				}

				var able bool
				var ableReason string
				if able, ableReason = as.AbleToRun(j); !able {
					unschedule = true
					if ableReason == job.JobReschedule {
						reason = ableReason
					} else {
						reason = fmt.Sprintf("target Machine(%s) unable to run unit", j.TargetMachineID)
						metrics.ReportEngineReconcileFailure(metrics.RunFailure)
					}
					return
				}

				return
			}

			unschedule, reason := decide()
			if !unschedule {
				continue
			}

			if !send(taskTypeUnscheduleUnit, reason, j.Name, j.TargetMachineID) {
				return
			}

			clust.unschedule(j.Name)
		}

		for _, j := range clust.jobs {
			if j.Scheduled() || j.TargetState == job.JobStateInactive {
				continue
			}

			dec, err := r.sched.Decide(clust, j)
			if err != nil {
				log.Debugf("Unable to schedule Job(%s): %v", j.Name, err)
				metrics.ReportEngineReconcileFailure(metrics.ScheduleFailure)
				continue
			}

			reason := fmt.Sprintf("target state %s and unit not scheduled", j.TargetState)
			if !send(taskTypeAttemptScheduleUnit, reason, j.Name, dec.machineID) {
				metrics.ReportEngineReconcileFailure(metrics.ScheduleFailure)
				return
			}

			clust.schedule(j.Name, dec.machineID)
		}
	}()

	return
}

func doTask(t *task, e *Engine) (err error) {
	switch t.Type {
	case taskTypeUnscheduleUnit:
		err = e.unscheduleUnit(t.JobName, t.MachineID)
		metrics.ReportEngineTask(t.Type)
	case taskTypeAttemptScheduleUnit:
		e.attemptScheduleUnit(t.JobName, t.MachineID)
		metrics.ReportEngineTask(t.Type)
	default:
		err = fmt.Errorf("unrecognized task type %q", t.Type)
	}

	if err == nil {
		log.Infof("EngineReconciler completed task: %s", t)
	} else {
		metrics.ReportEngineTaskFailure(t.Type)
	}

	return
}
