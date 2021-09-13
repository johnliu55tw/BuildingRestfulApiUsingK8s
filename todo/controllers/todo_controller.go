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

	// TEST CREATE JOB
	job, err := constructNotificationJob(&todo, time.Now())
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

	/* 2. Update status from notification Job */

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TodoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&todov1.Todo{}).
		Complete(r)
}

func constructNotificationJob(todo *todov1.Todo, scheduledTime time.Time) (*batchv1.Job, error) {
	// We want job names for a given nominal start time to have a deterministic name to avoid the same job being created twice
	name := fmt.Sprintf("%s-%d", todo.Name, scheduledTime.Unix())

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
								fmt.Sprintf("echo 'This is notification for %s'", todo.Spec.NotifyEmail),
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}
	return job, nil
}
