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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	todov1 "johnliu55.tw/todo/api/v1"
)

var (
	scheduledTimeAnnotation = "todo.johnliu55.tw/scheduled-at"
	jobOwnerKey             = ".metadata.controller"
	apiGVStr                = todov1.GroupVersion.String()
)

// TodoReconciler reconciles a Todo object
type TodoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=todo.johnliu55.tw,resources=todoes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=todo.johnliu55.tw,resources=todoes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=todo.johnliu55.tw,resources=todoes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Todo object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *TodoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Reconcile func", "NamespacesName", req.NamespacedName)

	/* 1. Load named Todo */
	logger.V(1).Info("Loading Todo")
	var todo todov1.Todo
	if err := r.Get(ctx, req.NamespacedName, &todo); err != nil {
		logger.Error(err, "unable to fetch Todo")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.V(1).Info("Todo loaded", "todo", todo)

	/* 2. Update status from notification Job */
	// Get jobs using labels
	var notiJobs batchv1.JobList
	if err := r.List(
		ctx,
		&notiJobs,
		client.InNamespace(req.Namespace),
		client.MatchingLabels{"todo": req.Name}); err != nil {

		logger.Error(err, "unable to list child Jobs")
		return ctrl.Result{}, err
	}
	logger.V(1).Info(fmt.Sprintf("Children jobs: %d", len(notiJobs.Items)))

	// If the todo has children jobs, update NotifiedAt if any of it completed.
	// If still running, return and wait for it to complete.
	for _, j := range notiJobs.Items {
		for idx, cond := range j.Status.Conditions {
			logger.V(1).Info(fmt.Sprintf("Cond %d type: %s", idx, cond.Type))
			switch cond.Type {
			case batchv1.JobComplete:
				logger.V(1).Info("Job completed!")
				todo.Status.NotifiedAt = cond.LastTransitionTime.DeepCopy()
				if err := r.Status().Update(ctx, &todo); err != nil {
					logger.Error(err, "Unable to update todo status")
					return ctrl.Result{}, err
				}
				return ctrl.Result{}, nil
			case batchv1.JobFailed:
				//TODO JOB FAILED! We should create a Job sometimes after
				logger.V(1).Info("Job failed!")
				return ctrl.Result{}, nil
			}
		}
		//XXX No conditions means the job is still running, so don't do anything
		logger.V(1).Info("Job still running...")
		return ctrl.Result{}, nil
	}

	/* 3. Create notify job*/
	// assert len(notiJobs.Items) == 0
	if todo.Spec.NotifyAt != nil {
		duraUntilScheduled := time.Until(todo.Spec.NotifyAt.Time)
		logger.V(1).Info(fmt.Sprintf("Duration until scheduled: %f mins", duraUntilScheduled.Minutes()))
		//TODO: Fix the constant here
		if duraUntilScheduled.Minutes() < -1.0 {
			logger.V(1).Info(fmt.Sprintf("SCHEDULE IN THE PAST: %v", todo.Spec.NotifyAt.Time))
			return ctrl.Result{}, nil
		}

		if math.Abs(duraUntilScheduled.Minutes()) < 1 {
			logger.V(1).Info("Within 1 mins, create notification job")
			job, err := r.constructNotificationJob(&todo)
			logger.V(1).Info("Job created", "job", job)
			if err != nil {
				logger.Error(err, "failed to create job from todo item")
				return ctrl.Result{}, nil
			}

			if err := r.Create(ctx, job); err != nil {
				logger.Error(err, "unable to create Job for CronJob", "job", job)
				return ctrl.Result{}, err
			}
			logger.V(1).Info("Notification job created")
			return ctrl.Result{}, nil
		}

		logger.V(1).Info(fmt.Sprintf("Schedule to run after %v mins", duraUntilScheduled.Minutes()))
		return ctrl.Result{RequeueAfter: duraUntilScheduled}, nil
	}

	/* TEST CREATE JOB
	job, err := r.constructNotificationJob(&todo, time.Now())
	logger.V(1).Info("Job created", "job", job)
	if err != nil {
		logger.Error(err, "failed to create job from todo item")
		return ctrl.Result{}, nil
	}

	if err := r.Create(ctx, job); err != nil {
		logger.Error(err, "unable to create Job for CronJob", "job", job)
		return ctrl.Result{}, err
	}
	logger.V(1).Info("Notification job created")
	*/

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TodoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&todov1.Todo{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

func (r *TodoReconciler) constructNotificationJob(todo *todov1.Todo) (*batchv1.Job, error) {
	// We want job names for a given nominal start time to have a deterministic name to avoid the same job being created twice
	name := fmt.Sprintf("%s-%d", todo.Name, time.Now().Unix())

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      make(map[string]string),
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   todo.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  fmt.Sprintf("%s-notify", name),
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

	job.Labels["todo"] = todo.Name
	if err := ctrl.SetControllerReference(todo, job, r.Scheme); err != nil {
		return nil, err
	}
	return job, nil
}
