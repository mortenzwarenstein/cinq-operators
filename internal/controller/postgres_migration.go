package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	databasev1 "cinqict.nl/operators/api/v1"
)

// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

const waitForHosts = `until pg_isready -q -h "$SRC" && pg_isready -q -h "$DST"; do sleep 2; done
`

const setupReplicationScript = waitForHosts + `
[ "$(psql -h "$SRC" -tAc 'show wal_level')" = logical ] || { echo "wal_level must be logical"; exit 1; }
psql -h "$SRC" -v ON_ERROR_STOP=1 -c 'CREATE PUBLICATION migration FOR ALL TABLES'
pg_dump -h "$SRC" --schema-only | psql -h "$DST" -v ON_ERROR_STOP=1
psql -h "$DST" -v ON_ERROR_STOP=1 -c "CREATE SUBSCRIPTION migration CONNECTION 'host=$SRC dbname=$PGDATABASE user=$PGUSER password=$PGPASSWORD' PUBLICATION migration"
`

const waitForSyncScript = waitForHosts + `
until [ "$(psql -h "$DST" -tAc "select count(*) from pg_subscription_rel where srsubstate <> 'r'")" = 0 ]; do sleep 2; done
`

const finalSyncScript = waitForHosts + `
psql -h "$SRC" -v ON_ERROR_STOP=1 -c "ALTER DATABASE \"$PGDATABASE\" SET default_transaction_read_only = on"
psql -h "$SRC" -v ON_ERROR_STOP=1 -c "select pg_terminate_backend(pid) from pg_stat_activity where datname = current_database() and backend_type = 'client backend' and pid <> pg_backend_pid()"
until [ "$(psql -h "$SRC" -tAc "select count(*) from pg_replication_slots where slot_name = 'migration' and pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn) <= 0")" = 1 ]; do sleep 1; done
psql -h "$SRC" -tAc "select format('select setval(%L, %s, true);', schemaname || '.' || sequencename, last_value) from pg_sequences where last_value is not null" | psql -h "$DST" -v ON_ERROR_STOP=1
`

const stopReplicationScript = waitForHosts + `
psql -h "$DST" -v ON_ERROR_STOP=1 -c 'DROP SUBSCRIPTION migration'
`

func (r *PostgresReconciler) Migrate(ctx context.Context, postgres *databasev1.Postgres) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	wait := ctrl.Result{RequeueAfter: 5 * time.Second}
	log.Info("Migrating Postgres", "from", postgres.Status.CurrentVersion, "to", postgres.Spec.Version)

	if done, err := r.StartTarget(ctx, postgres); err != nil || !done {
		return wait, err
	}

	if done, err := r.StartReplication(ctx, postgres); err != nil || !done {
		return wait, err
	}

	if done, err := r.WaitForSync(ctx, postgres); err != nil || !done {
		return wait, err
	}

	if done, err := r.FinalSync(ctx, postgres); err != nil || !done {
		return wait, err
	}

	if err := r.SwitchTraffic(ctx, postgres); err != nil {
		return ctrl.Result{}, err
	}

	if done, err := r.StopReplication(ctx, postgres); err != nil || !done {
		return wait, err
	}

	if err := r.RemoveSource(ctx, postgres); err != nil {
		return ctrl.Result{}, err
	}

	postgres.Status.CurrentVersion = postgres.Spec.Version
	if err := r.Status().Update(ctx, postgres); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("Migrated Postgres", "version", postgres.Spec.Version)
	return ctrl.Result{}, nil
}

func (r *PostgresReconciler) EnsureClientService(ctx context.Context, postgres *databasev1.Postgres) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clientServiceName(postgres),
			Namespace: postgres.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		if svc.CreationTimestamp.IsZero() {
			svc.Spec.Selector = map[string]string{"statefulset.kubernetes.io/pod-name": sourceName(postgres) + "-0"}
			svc.Spec.Ports = []corev1.ServicePort{
				{
					Name:       "postgres",
					Port:       5432,
					TargetPort: intstr.FromString("postgres"),
				},
			}
		}
		return controllerutil.SetControllerReference(postgres, svc, r.Scheme)
	})
	return err
}

func (r *PostgresReconciler) StartTarget(ctx context.Context, postgres *databasev1.Postgres) (bool, error) {
	return r.ensureDatabase(ctx, postgres, postgres.Spec.Version)
}

func (r *PostgresReconciler) StartReplication(ctx context.Context, postgres *databasev1.Postgres) (bool, error) {
	return r.runJob(ctx, postgres, "replicate", setupReplicationScript)
}

func (r *PostgresReconciler) WaitForSync(ctx context.Context, postgres *databasev1.Postgres) (bool, error) {
	return r.runJob(ctx, postgres, "wait-sync", waitForSyncScript)
}

func (r *PostgresReconciler) FinalSync(ctx context.Context, postgres *databasev1.Postgres) (bool, error) {
	return r.runJob(ctx, postgres, "final-sync", finalSyncScript)
}

func (r *PostgresReconciler) SwitchTraffic(ctx context.Context, postgres *databasev1.Postgres) error {
	svc := &corev1.Service{}
	key := types.NamespacedName{Name: clientServiceName(postgres), Namespace: postgres.Namespace}
	if err := r.Get(ctx, key, svc); err != nil {
		return err
	}
	svc.Spec.Selector = map[string]string{"statefulset.kubernetes.io/pod-name": targetName(postgres) + "-0"}
	return r.Update(ctx, svc)
}

func (r *PostgresReconciler) StopReplication(ctx context.Context, postgres *databasev1.Postgres) (bool, error) {
	return r.runJob(ctx, postgres, "stop-replication", stopReplicationScript)
}

func (r *PostgresReconciler) RemoveSource(ctx context.Context, postgres *databasev1.Postgres) error {
	src := sourceName(postgres)
	objects := []client.Object{
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: src, Namespace: postgres.Namespace}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data-" + src + "-0", Namespace: postgres.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: src, Namespace: postgres.Namespace}},
	}
	for _, step := range []string{"replicate", "wait-sync", "final-sync", "stop-replication"} {
		objects = append(objects, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName(postgres, step), Namespace: postgres.Namespace}})
	}
	for _, obj := range objects {
		if err := r.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationBackground)); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

func (r *PostgresReconciler) runJob(ctx context.Context, postgres *databasev1.Postgres, step, script string) (bool, error) {
	src := sourceName(postgres)
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName(postgres, step), Namespace: postgres.Namespace}}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, job, func() error {
		if job.CreationTimestamp.IsZero() {
			job.Spec = jobSpec(postgres, src, script)
		}
		return controllerutil.SetControllerReference(postgres, job, r.Scheme)
	})
	if err != nil {
		return false, err
	}

	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			return true, nil
		case batchv1.JobFailed:
			return false, fmt.Errorf("job %s failed: %s", job.Name, c.Message)
		}
	}
	return false, nil
}

func jobSpec(postgres *databasev1.Postgres, src, script string) batchv1.JobSpec {
	backoff := int32(0)
	secret := corev1.LocalObjectReference{Name: postgres.Name + "-credentials"}
	fromSecret := func(key string) *corev1.EnvVarSource {
		return &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: secret, Key: key}}
	}
	return batchv1.JobSpec{
		BackoffLimit: &backoff,
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{{
					Name:    "migrate",
					Image:   fmt.Sprintf("postgres:%d", postgres.Spec.Version),
					Command: []string{"sh", "-c", "set -eu\n" + script},
					Env: []corev1.EnvVar{
						{Name: "PGUSER", ValueFrom: fromSecret("POSTGRES_USER")},
						{Name: "PGPASSWORD", ValueFrom: fromSecret("POSTGRES_PASSWORD")},
						{Name: "PGDATABASE", ValueFrom: fromSecret("POSTGRES_DB")},
						{Name: "SRC", Value: podHost(postgres, src)},
						{Name: "DST", Value: podHost(postgres, targetName(postgres))},
					},
				}},
			},
		},
	}
}

func targetName(postgres *databasev1.Postgres) string {
	return versionedName(postgres, postgres.Spec.Version)
}

func currentVersion(postgres *databasev1.Postgres) int {
	if postgres.Status.CurrentVersion != 0 {
		return postgres.Status.CurrentVersion
	}
	return postgres.Spec.Version
}

func sourceName(postgres *databasev1.Postgres) string {
	return versionedName(postgres, currentVersion(postgres))
}

func versionedName(postgres *databasev1.Postgres, version int) string {
	return fmt.Sprintf("%s-%d", postgres.Name, version)
}

func clientServiceName(postgres *databasev1.Postgres) string { return postgres.Name + "-primary" }

func jobName(postgres *databasev1.Postgres, step string) string {
	return fmt.Sprintf("%s-migrate-%s", postgres.Name, step)
}

func podHost(postgres *databasev1.Postgres, stsName string) string {
	return fmt.Sprintf("%s-0.%s.%s.svc", stsName, stsName, postgres.Namespace)
}
