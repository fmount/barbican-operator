package barbicanworker

import (
	"slices"

	"github.com/openstack-k8s-operators/lib-common/modules/common/env"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//"k8s.io/apimachinery/pkg/util/intstr"

	barbicanv1beta1 "github.com/openstack-k8s-operators/barbican-operator/api/v1beta1"
	barbican "github.com/openstack-k8s-operators/barbican-operator/pkg/barbican"
	topologyv1 "github.com/openstack-k8s-operators/infra-operator/apis/topology/v1beta1"
)

const (
	// ServiceCommand -
	ServiceCommand = "/usr/local/bin/kolla_start"
)

// Deployment - returns a BarbicanWorker Deployment
func Deployment(
	instance *barbicanv1beta1.BarbicanWorker,
	configHash string,
	labels map[string]string,
	annotations map[string]string,
	topology *topologyv1.Topology,
) *appsv1.Deployment {
	runAsUser := int64(0)
	var config0644AccessMode int32 = 0644
	envVars := map[string]env.Setter{}
	envVars["KOLLA_CONFIG_STRATEGY"] = env.SetValue("COPY_ALWAYS")
	envVars["CONFIG_HASH"] = env.SetValue(configHash)
	/*
		livenessProbe := &corev1.Probe{
			// TODO might need tuning
			TimeoutSeconds:      5,
			PeriodSeconds:       3,
			InitialDelaySeconds: 5,
		}
		readinessProbe := &corev1.Probe{
			// TODO might need tuning
			TimeoutSeconds:      5,
			PeriodSeconds:       5,
			InitialDelaySeconds: 5,
		}
	*/
	args := []string{"-c", ServiceCommand}
	//
	// https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
	//
	//livenessProbe.HTTPGet = &corev1.HTTPGetAction{
	//	Path: "/healthcheck",
	//	Port: intstr.IntOrString{Type: intstr.Int, IntVal: int32(barbican.BarbicanPublicPort)},
	//}
	//readinessProbe.HTTPGet = livenessProbe.HTTPGet

	workerVolumes := []corev1.Volume{
		{
			Name: "config-data-custom",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					DefaultMode: &config0644AccessMode,
					SecretName:  instance.Name + "-config-data",
				},
			},
		},
		barbican.GetLogVolume(),
	}

	workerVolumeMounts := []corev1.VolumeMount{
		barbican.GetKollaConfigVolumeMount(instance.Name),
		barbican.GetLogVolumeMount(),
	}

	// Add the CA bundle
	if instance.Spec.TLS.CaBundleSecretName != "" {
		workerVolumes = append(workerVolumes, instance.Spec.TLS.CreateVolume())
		workerVolumeMounts = append(workerVolumeMounts, instance.Spec.TLS.CreateVolumeMounts(nil)...)
	}

	// Add PKCS11 volumes
	if slices.Contains(instance.Spec.EnabledSecretStores, barbicanv1beta1.SecretStorePKCS11) && instance.Spec.PKCS11 != nil {
		workerVolumes = append(workerVolumes, barbican.GetHSMVolumes(*instance.Spec.PKCS11)...)
		workerVolumeMounts = append(workerVolumeMounts, barbican.GetHSMVolumeMounts()...)
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Replicas: instance.Spec.Replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotations,
					Labels:      labels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: instance.Spec.ServiceAccount,
					Containers: []corev1.Container{
						{
							Name: instance.Name + "-log",
							Command: []string{
								"/usr/bin/dumb-init",
							},
							Args: []string{
								"--single-child",
								"--",
								"/usr/bin/tail",
								"-n+1",
								"-F",
								barbican.BarbicanLogPath + instance.Name + ".log",
							},
							Image: instance.Spec.ContainerImage,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: &runAsUser,
							},
							Env:          env.MergeEnvs([]corev1.EnvVar{}, envVars),
							VolumeMounts: []corev1.VolumeMount{barbican.GetLogVolumeMount()},
							Resources:    instance.Spec.Resources,
							//ReadinessProbe: readinessProbe,
							//LivenessProbe:  livenessProbe,
						},
						{
							Name: barbican.ServiceName + "-worker",
							Command: []string{
								"/bin/bash",
							},
							Args:  args,
							Image: instance.Spec.ContainerImage,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser: &runAsUser,
							},
							Env: env.MergeEnvs([]corev1.EnvVar{}, envVars),
							VolumeMounts: append(barbican.GetVolumeMounts(
								instance.Spec.CustomServiceConfigSecrets),
								workerVolumeMounts...,
							),
							Resources: instance.Spec.Resources,
							//ReadinessProbe: readinessProbe,
							//LivenessProbe:  livenessProbe,
						},
					},
				},
			},
		},
	}
	deployment.Spec.Template.Spec.Volumes = append(barbican.GetVolumes(
		instance.Name,
		instance.Spec.CustomServiceConfigSecrets),
		workerVolumes...)

	if instance.Spec.NodeSelector != nil {
		deployment.Spec.Template.Spec.NodeSelector = *instance.Spec.NodeSelector
	}

	if topology != nil {
		topology.ApplyTo(&deployment.Spec.Template)
	} else {
		// If possible two pods of the same service should not
		// run on the same worker node. If this is not possible
		// the get still created on the same worker node.
		deployment.Spec.Template.Spec.Affinity = barbican.GetPodAffinity(barbican.ComponentWorker)
	}

	return deployment
}
