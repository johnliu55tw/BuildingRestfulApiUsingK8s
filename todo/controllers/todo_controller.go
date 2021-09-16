/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"math"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	todov1 "johnliu55.tw/todo/api/v1"
)

// States
type state int8

const (
	Updated state = iota
	ShouldSchedule
	ShouldNotify
	Notifying
	Notified
	StateError
)

func (s state) String() string {
	switch s {
	case Updated:
		return "Updated"
	case ShouldSchedule:
		return "ShouldSchedule"
	case ShouldNotify:
		return "ShouldNotify"
	case Notifying:
		return "Notifying"
	case Notified:
		return "Notified"
	default:
		return "Unknown State"
	}
}

var (
	scheduledTimeAnnotation = "todo.johnliu55.tw/scheduled-at"
)

// TodoReconciler reconciles a Todo object
type TodoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Get the time the job meant to run, using Annotation.
// Note: It's not the time the job actually ran!
func getScheduledTimeOfJob(job *batchv1.Job) (time.Time, error) {
	timeRaw := job.Annotations[scheduledTimeAnnotation]
	if len(timeRaw) == 0 {
		return time.Time{}, nil
	}

	timeParsed, err := time.Parse(time.RFC3339, timeRaw)
	if err != nil {
		return time.Time{}, err
	}
	return timeParsed, nil
}

//+kubebuilder:rbac:groups=todo.johnliu55.tw,resources=todoes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=todo.johnliu55.tw,resources=todoes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=todo.johnliu55.tw,resources=todoes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *TodoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Reconcile func", "NamespacesName", req.NamespacedName)

	// Init
	// Load named Todo
	logger.V(1).Info("Loading Todo")
	var todo = new(todov1.Todo)
	if err := r.Get(ctx, req.NamespacedName, todo); err != nil {
		logger.Error(err, "unable to fetch Todo")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.V(1).Info("Todo loaded", "todo", *todo)

	// Load notification job
	objKey := r.getNotificationJobKey(todo)
	var notiJob *batchv1.Job = new(batchv1.Job)
	if err := r.Get(ctx, objKey, notiJob); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("Job not found")
			notiJob = nil
		} else {
			return ctrl.Result{}, err
		}
	}

	// Determine current Todo state
	state, err := r.determineCurrentState(ctx, todo, notiJob)
	if err != nil {
		logger.V(1).Error(err, "Error determining state")
	}
	logger.V(1).Info(fmt.Sprintf("Current state: %s", state))

	// Do stuff based on determined state
	switch state {
	case Updated:
		// Do nothing
	case ShouldSchedule:
		// Notification time is still far from now, so use RequeueAfter to re-run at that time
		duraUntilScheduled := time.Until(todo.Spec.NotifyAt.Time)
		logger.Info(fmt.Sprintf("Requeue notification after: %v mins", duraUntilScheduled.Minutes()))
		return ctrl.Result{RequeueAfter: duraUntilScheduled}, nil
	case ShouldNotify:
		// It's time to create a Job to send notification
		job, err := r.constructNotificationJob(todo, todo.Spec.NotifyAt.Time)
		if err != nil {
			// If failed to create job object, ignore
			logger.Error(err, "failed to construct job from todo item")
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, job); err != nil {
			logger.Error(err, "unable to create job", "job", job)
			return ctrl.Result{}, err
		}
		logger.Info("Notification job created")
		return ctrl.Result{}, nil
	case Notifying:
		// Do nothiing
	case Notified:
		// Notification job completed/failed. Update the Status of Todo.
		scheduledTime, err := getScheduledTimeOfJob(notiJob)
		if err != nil {
			logger.Error(err, "unable to get scheduled time of job")
		}

		logger.Info("notified. Update NotifiedAt time")
		todo.Status.NotifiedAt = &metav1.Time{Time: scheduledTime}
		if err := r.Status().Update(ctx, todo); err != nil {
			logger.Error(err, "Unable to update todo status")
			return ctrl.Result{}, err
		}

		logger.Info("delete the notification job")
		if err := r.Delete(ctx, notiJob, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			logger.Error(err, "failed to delete job")
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TodoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&todov1.Todo{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func (r *TodoReconciler) determineCurrentState(ctx context.Context, todo *todov1.Todo, notiJob *batchv1.Job) (state, error) {
	logger := log.FromContext(ctx).WithName("determineCurrentState")
	logger.V(1).Info("DETERMINE STATE")

	//XXX No conditions means the job is still running, so don't do anything
	if notiJob != nil && len(notiJob.Status.Conditions) == 0 {
		scheduledTime, err := getScheduledTimeOfJob(notiJob)
		if err != nil {
			logger.Error(err, "unable to get scheduledTime of the job")
			return StateError, err
		}

		logger.V(1).Info("Job still running...")
		if scheduledTime != todo.Spec.NotifyAt.Time && todo.Spec.NotifyAt.After(time.Now()) {
			// Users changed the notify time while the job is still sending notification.
			// In this case we should send the notification again on specified time,
			// **if the time is in the future.**
			return ShouldSchedule, nil
		}
		// Still notifying, wait for the job completed event
		return Notifying, nil
	}

	//XXX Job has conditions means either completed or failed
	if notiJob != nil {
		for _, cond := range notiJob.Status.Conditions {
			switch cond.Type {
			case batchv1.JobComplete:
				logger.V(1).Info("Job completed!")
				return Notified, nil
			case batchv1.JobFailed:
				//TODO JOB FAILED! We should create a Job sometimes after
				logger.V(1).Info("Job failed!")
				return Notified, nil
			}
		}
	}

	//XXX No job: This reconcile is triggered by Create/Update/Delete/JobDelete/StatusUpdate
	// The Todo is complete or notifyAt is not specified, so no need to notify
	if todo.Spec.Complete || todo.Spec.NotifyAt == nil {
		return Updated, nil
	}

	if todo.Status.NotifiedAt == nil || !todo.Status.NotifiedAt.Time.Equal(todo.Spec.NotifyAt.Time) {
		// No notification sent time or notification sent time != specified notify time
		if math.Abs(time.Until(todo.Spec.NotifyAt.Time).Minutes()) < 1 {
			logger.V(1).Info("NotifyAt within tolerance")
			return ShouldNotify, nil
		}
		if todo.Spec.NotifyAt.Time.After(time.Now()) {
			logger.V(1).Info("Schedule still far away")
			return ShouldSchedule, nil
		}
	} else {
		// Notification sent time equal to specified notification time
		// Do nothing
		logger.V(1).Info(fmt.Sprintf(
			"Equal: %t", todo.Spec.NotifyAt.Time.Equal(todo.Status.NotifiedAt.Time),
		))
	}

	return Updated, nil
}

func (r *TodoReconciler) constructNotificationJob(todo *todov1.Todo, scheduledTime time.Time) (*batchv1.Job, error) {
	objKey := r.getNotificationJobKey(todo)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        objKey.Name,
			Namespace:   objKey.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  fmt.Sprintf("%s-pod", objKey.Name),
							Image: "busybox",
							Command: []string{"/bin/sh", "-c",
								fmt.Sprintf("sleep 10; echo 'This is notification for %s'", todo.Spec.NotifyEmail),
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}

	job.Annotations[scheduledTimeAnnotation] = scheduledTime.Format(time.RFC3339)
	job.Labels["todo"] = todo.Name
	if err := ctrl.SetControllerReference(todo, job, r.Scheme); err != nil {
		return nil, err
	}
	return job, nil
}

func (r *TodoReconciler) getNotificationJobKey(todo *todov1.Todo) client.ObjectKey {
	return client.ObjectKey{
		Namespace: todo.Namespace,
		Name:      fmt.Sprintf("%s-notification", todo.Name),
	}
}
