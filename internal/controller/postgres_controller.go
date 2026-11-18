/*
Copyright YEAR.

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

package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	databasev1 "cinqict.nl/operators/api/v1"
)

// PostgresReconciler reconciles a Postgres object
type PostgresReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=database.cinqict.nl,resources=postgres,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=database.cinqict.nl,resources=postgres/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=database.cinqict.nl,resources=postgres/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PostgresReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// haal onze eigen Postgres CR op uit het cluster
	postgres := &databasev1.Postgres{}
	if err := r.Get(ctx, req.NamespacedName, postgres); err != nil {
		// Als we geen CR kunnen vinden doen we niets (en geven we ook geen errors terug)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// We zetten een client service op, zodat de pods op een vast punt bereikbaar zijn
	if err := r.EnsureClientService(ctx, postgres); err != nil {
		return ctrl.Result{}, err
	}

	if r.MigrationNeeded(postgres) {
		return r.Migrate(ctx, postgres)
	}

	ready, err := r.ensureDatabase(ctx, postgres, currentVersion(postgres))
	if err != nil {
		return ctrl.Result{}, err
	}

	// zet de CurrentVersion zodat we deze later nog kunnen uitlezen en kunnen checken of een migration nodig is
	if postgres.Status.CurrentVersion == 0 {
		postgres.Status.CurrentVersion = postgres.Spec.Version
		if err := r.Status().Update(ctx, postgres); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info("StatefulSet reconciled", "ready", ready)
	return ctrl.Result{}, nil
}

func (r *PostgresReconciler) ensureDatabase(ctx context.Context, postgres *databasev1.Postgres, version int) (bool, error) {
	name := versionedName(postgres, version)
	if err := r.ensureHeadlessService(ctx, postgres, name); err != nil {
		return false, err
	}

	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgres.Namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, sts, func() error {
		if sts.CreationTimestamp.IsZero() {
			sts.Spec = statefulSetSpec(postgres, version)
		}
		return controllerutil.SetControllerReference(postgres, sts, r.Scheme)
	}); err != nil {
		return false, err
	}
	return sts.Status.ReadyReplicas == 1, nil
}

func statefulSetSpec(postgres *databasev1.Postgres, version int) appsv1.StatefulSetSpec {
	name := fmt.Sprintf("%s-%d", postgres.Name, version)
	labels := map[string]string{"app": name}
	replicas := int32(1)
	pvc := getPVC()

	return appsv1.StatefulSetSpec{
		ServiceName: name,
		Replicas:    &replicas,
		Selector:    &metav1.LabelSelector{MatchLabels: labels},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{postgresContainer(postgres, version)},
			},
		},
		VolumeClaimTemplates: []corev1.PersistentVolumeClaim{pvc},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PostgresReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&databasev1.Postgres{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&batchv1.Job{}).
		Owns(&corev1.Service{}).
		Named("postgres").
		Complete(r)
}
