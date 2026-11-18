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

// Kant-en-klare bouwblokken voor de workshop. Deze hoef je niet aan te passen:
// ensureDatabase en statefulSetSpec in postgres_controller.go zetten ze in elkaar.

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	databasev1 "cinqict.nl/operators/api/v1"
)

// ensureHeadlessService zorgt voor de headless Service die elke pod een vaste DNS-naam geeft.
func (r *PostgresReconciler) ensureHeadlessService(ctx context.Context, postgres *databasev1.Postgres, name string) error {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: postgres.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Selector = map[string]string{"app": name}
		svc.Spec.Ports = []corev1.ServicePort{{Name: "postgres", Port: 5432, TargetPort: intstr.FromString("postgres")}}
		return controllerutil.SetControllerReference(postgres, svc, r.Scheme)
	})
	return err
}

// postgresContainer is een kant-en-klaar bouwblok: de Postgres container voor de gegeven versie.
func postgresContainer(postgres *databasev1.Postgres, version int) corev1.Container {
	return corev1.Container{
		Name:  "postgres",
		Image: fmt.Sprintf("postgres:%d", version),
		Args:  []string{"-c", "wal_level=logical"}, // vereist voor logical replication bij migratie
		Ports: []corev1.ContainerPort{{Name: "postgres", ContainerPort: 5432}},
		EnvFrom: []corev1.EnvFromSource{{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: postgres.Name + "-credentials"},
			},
		}},
		Env:          []corev1.EnvVar{{Name: "PGDATA", Value: "/var/lib/postgresql/data/pgdata"}},
		VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/var/lib/postgresql/data"}},
	}
}

func getPVC() corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data"},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
		},
	}
}
