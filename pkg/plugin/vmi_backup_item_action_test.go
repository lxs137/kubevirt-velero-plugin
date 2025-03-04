package plugin

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	velerov1 "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/kuberesource"
	"github.com/vmware-tanzu/velero/pkg/plugin/velero"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kvcore "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/kubevirt-velero-plugin/pkg/util"
)

func TestVMIBackupItemAction(t *testing.T) {
	returnFalse := func(something ...interface{}) (bool, error) { return false, nil }
	returnTrue := func(something ...interface{}) (bool, error) { return true, nil }

	nullValidator := func(runtime.Unstructured, []velero.ResourceIdentifier) bool { return true }

	ownedVMI := map[string]interface{}{
		"apiVersion": "kubevirt.io",
		"kind":       "VirtualMachineInterface",
		"metadata": map[string]interface{}{
			"name":      "test-vmi",
			"namespace": "test-namespace",
			"ownerReferences": []interface{}{
				map[string]interface{}{
					"name": "test-owner",
				},
			},
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"volumes": []map[string]interface{}{},
				},
			},
		},
	}
	nonOwnedVMI := map[string]interface{}{
		"apiVersion": "kubevirt.io",
		"kind":       "VirtualMachineInterface",
		"metadata": map[string]interface{}{
			"name":      "test-vmi",
			"namespace": "test-namespace",
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"volumes": []map[string]interface{}{},
				},
			},
		},
	}
	pausedVMI := map[string]interface{}{
		"apiVersion": "kubevirt.io",
		"kind":       "VirtualMachineInterface",
		"metadata": map[string]interface{}{
			"name":      "test-vmi",
			"namespace": "test-namespace",
			"ownerReferences": []interface{}{
				map[string]interface{}{
					"name": "test-owner",
				},
			},
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"volumes": []map[string]interface{}{},
				},
			},
		},
		"status": map[string]interface{}{
			"conditions": []map[string]interface{}{
				{
					"type":   "Paused",
					"status": "True",
				},
			},
		},
	}

	launcherPod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
			Name:      "test-vmi-launcher-pod",
			Labels: map[string]string{
				"kubevirt.io": "virt-launcher",
			},
			Annotations: map[string]string{
				"kubevirt.io/domain": "test-vmi",
			},
		},
	}
	excludedPod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
			Name:      "test-vmi-launcher-pod",
			Labels: map[string]string{
				"kubevirt.io":                   "virt-launcher",
				"velero.io/exclude-from-backup": "true",
			},
			Annotations: map[string]string{
				"kubevirt.io/domain": "test-vmi",
			},
		},
	}

	testCases := []struct {
		name          string
		item          unstructured.Unstructured
		backup        velerov1.Backup
		pod           v1.Pod
		isPvcExcluded func(something ...interface{}) (bool, error)
		isVmExcluded  func(something ...interface{}) (bool, error)
		expectError   bool
		errorMsg      string
		validator     func(runtime.Unstructured, []velero.ResourceIdentifier) bool
	}{
		{"Paused VMI can exclude pods from backup",
			unstructured.Unstructured{
				Object: pausedVMI,
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					ExcludedResources: []string{"pods"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			false,
			"",
			nullValidator,
		},
		{"Paused VMI can omit Pod in included resources",
			unstructured.Unstructured{
				Object: pausedVMI,
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					IncludedResources: []string{"virtualmachineinstances", "virtualmachines"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			false,
			"",
			nullValidator,
		},
		{"Paused VMI can exclude Pod by label",
			unstructured.Unstructured{
				Object: pausedVMI,
			},
			velerov1.Backup{},
			excludedPod,
			returnFalse,
			returnFalse,
			false,
			"",
			nullValidator,
		},
		{"Running VMI must include Pod in backup",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					IncludedResources: []string{"virtualmachineinstances", "virtualmachines", "persistentvolumeclaims"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			true,
			"VM is running but launcher pod is not included in the backup",
			nullValidator,
		},
		{"Running VMI must not exclude Pods",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					ExcludedResources: []string{"pods"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			true,
			"VM is running but launcher pod is not included in the backup",
			nullValidator,
		},
		{"Running VMI must not exclude its Pod by label",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{},
			excludedPod,
			returnFalse,
			returnTrue,
			true,
			"VM is running but launcher pod is not included in the backup",
			nullValidator,
		},
		{"Running VMI must include Pod in backup unless it does not include PVCs",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					IncludedResources: []string{"virtualmachineinstances", "virtualmachines"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			false,
			"",
			nullValidator,
		},
		{"Running VMI must not exclude Pods unless it also excludes PVCs",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					ExcludedResources: []string{"pods", "persistentvolumeclaims"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			false,
			"",
			nullValidator,
		},
		{"Owned VMI: backup must include VM",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					IncludedResources: []string{"virtualmachineinstances"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			true,
			"VMI owned by a VM and the VM is not included in the backup",
			nullValidator,
		},
		{"Owned VMI: VM must not be excluded from backup",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					ExcludedResources: []string{"virtualmachines"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			true,
			"VMI owned by a VM and the VM is not included in the backup",
			nullValidator,
		},
		{"Owned VMI: VM must not be excluded by label",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{},
			launcherPod,
			returnFalse,
			returnTrue,
			true,
			"VMI owned by a VM and the VM is not included in the backup",
			nullValidator,
		},
		{"Owned VMI must add 'is owned' annotation",
			unstructured.Unstructured{
				Object: ownedVMI,
			},
			velerov1.Backup{},
			launcherPod,
			returnFalse,
			returnFalse,
			false,
			"",
			func(item runtime.Unstructured, extra []velero.ResourceIdentifier) bool {
				metadata, err := meta.Accessor(item)
				assert.NoError(t, err)

				return assert.Equal(t, map[string]string{"cdi.kubevirt.io/velero.isOwned": "true"}, metadata.GetAnnotations())
			},
		},
		{"Not owned VMI with DV volumes must include DataVolumes in backup",
			unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "kubevirt.io",
					"kind":       "VirtualMachineInterface",
					"metadata": map[string]interface{}{
						"name":      "test-vmi",
						"namespace": "test-namespace",
					},
					"spec": map[string]interface{}{
						"volumes": []interface{}{
							map[string]interface{}{
								"dataVolume": map[string]interface{}{},
							},
						},
					},
				},
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					IncludedResources: []string{"virtualmachineinstances", "pods"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			true,
			"VM has DataVolume or PVC volumes and DataVolumes/PVCs is not included in the backup",
			nullValidator,
		},
		{"Not owned VMI with DV volumes must not exclude DataVolumes from backup",
			unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "kubevirt.io",
					"kind":       "VirtualMachineInterface",
					"metadata": map[string]interface{}{
						"name":      "test-vmi",
						"namespace": "test-namespace",
					},
					"spec": map[string]interface{}{
						"volumes": []interface{}{
							map[string]interface{}{
								"dataVolume": map[string]interface{}{},
							},
						},
					},
				},
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					ExcludedResources: []string{"datavolumes"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			true,
			"VM has DataVolume or PVC volumes and DataVolumes/PVCs is not included in the backup",
			nullValidator,
		},
		{"Not owned VMI with PVC volumes must include PVCs in backup",
			unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "kubevirt.io",
					"kind":       "VirtualMachineInterface",
					"metadata": map[string]interface{}{
						"name":      "test-vmi",
						"namespace": "test-namespace",
					},
					"spec": map[string]interface{}{
						"volumes": []interface{}{
							map[string]interface{}{
								"persistentVolumeClaim": map[string]interface{}{},
							},
						},
					},
				},
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					IncludedResources: []string{"virtualmachineinstances", "pods"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			true,
			"VM has DataVolume or PVC volumes and DataVolumes/PVCs is not included in the backup",
			nullValidator,
		},
		{"Not owned VMI with PVC volumes must not exclude PVCs from backup",
			unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "kubevirt.io",
					"kind":       "VirtualMachineInterface",
					"metadata": map[string]interface{}{
						"name":      "test-vmi",
						"namespace": "test-namespace",
					},
					"spec": map[string]interface{}{
						"volumes": []interface{}{
							map[string]interface{}{
								"persistentVolumeClaim": map[string]interface{}{},
							},
						},
					},
				},
			},
			velerov1.Backup{
				Spec: velerov1.BackupSpec{
					ExcludedResources: []string{"persistentvolumeclaims"},
				},
			},
			launcherPod,
			returnFalse,
			returnFalse,
			true,
			"VM has DataVolume or PVC volumes and DataVolumes/PVCs is not included in the backup",
			nullValidator,
		},
		{"Launcher pod included in extra resources",
			unstructured.Unstructured{
				Object: nonOwnedVMI,
			},
			velerov1.Backup{},
			launcherPod,
			returnFalse,
			returnFalse,
			false,
			"",
			func(_ runtime.Unstructured, extra []velero.ResourceIdentifier) bool {
				podResource := velero.ResourceIdentifier{
					GroupResource: kuberesource.Pods,
					Namespace:     "test-namespace",
					Name:          "test-vmi-launcher-pod",
				}
				return assert.Contains(t, extra, podResource)
			},
		},
		{"Volumes included in extra resources",
			unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "kubevirt.io",
					"kind":       "VirtualMachineInterface",
					"metadata": map[string]interface{}{
						"name":      "test-vmi",
						"namespace": "test-namespace",
						"ownerReferences": []interface{}{
							map[string]interface{}{
								"name": "test-owner",
							},
						},
					},
					"spec": map[string]interface{}{
						"volumes": []interface{}{
							map[string]interface{}{
								"persistentVolumeClaim": map[string]interface{}{
									"claimName": "test-pvc",
								},
							},
							map[string]interface{}{
								"dataVolume": map[string]interface{}{
									"name": "test-dv",
								},
							},
						},
					},
				},
			},
			velerov1.Backup{},
			launcherPod,
			returnFalse,
			returnFalse,
			false,
			"",
			func(_ runtime.Unstructured, extra []velero.ResourceIdentifier) bool {
				pvcResource := velero.ResourceIdentifier{
					GroupResource: kuberesource.PersistentVolumeClaims,
					Namespace:     "test-namespace",
					Name:          "test-pvc",
				}
				dvResource := velero.ResourceIdentifier{
					GroupResource: schema.GroupResource{
						Group:    "cdi.kubevirt.io",
						Resource: "datavolumes",
					},
					Namespace: "test-namespace",
					Name:      "test-dv",
				}

				assert.Contains(t, extra, pvcResource)
				assert.Contains(t, extra, dvResource)
				return true
			},
		},
	}

	logrus.SetLevel(logrus.ErrorLevel)
	for _, tc := range testCases {
		kubeobjects := []runtime.Object{}
		kubeobjects = append(kubeobjects, &tc.pod)
		client := k8sfake.NewSimpleClientset(kubeobjects...)
		action := NewVMIBackupItemAction(logrus.StandardLogger(), client)
		isVMExcludedByLabel = func(vmi *kvcore.VirtualMachineInstance) (bool, error) { return tc.isVmExcluded(vmi) }
		util.IsPVCExcludedByLabel = func(namespace, pvcName string) (bool, error) { return tc.isPvcExcluded(namespace, pvcName) }

		t.Run(tc.name, func(t *testing.T) {
			output, extra, err := action.Execute(&tc.item, &tc.backup)

			if tc.expectError {
				assert.Error(t, err)
				assert.Equal(t, tc.errorMsg, err.Error())
			} else {
				assert.NoError(t, err)
				tc.validator(output, extra)
			}
		})
	}
}

func TestAddLauncherPod(t *testing.T) {
	testCases := []struct {
		name     string
		vmi      kvcore.VirtualMachineInstance
		pods     v1.Pod
		expected []velero.ResourceIdentifier
	}{
		{"Should include launcher pod if present",
			kvcore.VirtualMachineInstance{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-vmi",
				},
			},
			v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-vmi-launcher-pod",
					Labels: map[string]string{
						"kubevirt.io": "virt-launcher",
					},
					Annotations: map[string]string{
						"kubevirt.io/domain": "test-vmi",
					},
				},
			},
			[]velero.ResourceIdentifier{
				{
					GroupResource: kuberesource.Pods,
					Namespace:     "test-namespace",
					Name:          "test-vmi-launcher-pod",
				},
			},
		},
		{"Should include only own launcher pod",
			kvcore.VirtualMachineInstance{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-vmi",
				},
			},
			v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-vmi-launcher-pod",
					Labels: map[string]string{
						"kubevirt.io": "virt-launcher",
					},
					Annotations: map[string]string{
						"kubevirt.io/domain": "another-vmi",
					},
				},
			},
			[]velero.ResourceIdentifier{},
		},
		{"Should not include other pods",
			kvcore.VirtualMachineInstance{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-vmi",
				},
			},
			v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
					Name:      "test-vmi-launcher-pod",
				},
			},
			[]velero.ResourceIdentifier{},
		},
	}

	logrus.SetLevel(logrus.ErrorLevel)
	kubecli.GetKubevirtClientFromClientConfig = kubecli.GetMockKubevirtClientFromClientConfig
	for _, tc := range testCases {
		kubeobjects := []runtime.Object{}
		kubeobjects = append(kubeobjects, &tc.pods)
		client := k8sfake.NewSimpleClientset(kubeobjects...)
		action := NewVMIBackupItemAction(logrus.StandardLogger(), client)

		t.Run(tc.name, func(t *testing.T) {
			output, err := action.addLauncherPod(&tc.vmi, []velero.ResourceIdentifier{})

			assert.NoError(t, err)
			assert.Equal(t, tc.expected, output)
		})
	}
}
