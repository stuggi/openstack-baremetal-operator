/*
Copyright 2023.

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

// Generated by:
//
// operator-sdk create webhook --group baremetal --version v1beta1 --kind OpenStackBaremetalSet --programmatic-validation
//

package v1beta1

import (
	"context"
	"fmt"

	"github.com/go-playground/validator/v10"
	metal3v1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	"github.com/openstack-k8s-operators/lib-common/modules/common/labels"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	goClient "sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// Client needed for API calls (manager's client, set by first SetupWebhookWithManager() call
// to any particular webhook)
var webhookClient goClient.Client

// log is for logging in this package.
var openstackbaremetalsetlog = logf.Log.WithName("openstackbaremetalset-resource")

// SetupWebhookWithManager - register this webhook with the controller manager
func (r *OpenStackBaremetalSet) SetupWebhookWithManager(mgr ctrl.Manager) error {
	if webhookClient == nil {
		webhookClient = mgr.GetClient()
	}

	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update;delete,path=/validate-baremetal-openstack-org-v1beta1-openstackbaremetalset,mutating=false,failurePolicy=fail,sideEffects=None,groups=baremetal.openstack.org,resources=openstackbaremetalsets,versions=v1beta1,name=vopenstackbaremetalset.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &OpenStackBaremetalSet{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *OpenStackBaremetalSet) ValidateCreate() (admission.Warnings, error) {
	openstackbaremetalsetlog.Info("validate create", "name", r.Name)
	var errors field.ErrorList
	// Check if OpenStackBaremetalSet name matches RFC1123 for use in labels
	validate := validator.New()
	if err := validate.Var(r.Name, "hostname_rfc1123"); err != nil {
		openstackbaremetalsetlog.Error(err, "Error validating OpenStackBaremetalSet name, name must follow RFC1123")
		errors = append(errors, field.Invalid(
			field.NewPath("Name"),
			r.Name,
			fmt.Sprintf("Error validating OpenStackBaremetalSet name %s, name must follow RFC1123", r.Name)))
	}

	//
	// Validate that there are enough available BMHs for the initial requested count
	//
	baremetalHostsList, err := GetBaremetalHosts(
		context.TODO(),
		webhookClient,
		r.Spec.BmhNamespace,
		r.Spec.BmhLabelSelector,
	)
	if err != nil {
		return nil, err
	}

	if _, err := VerifyBaremetalSetScaleUp(openstackbaremetalsetlog, r, baremetalHostsList, &metal3v1.BareMetalHostList{}); err != nil {
		return nil, err
	}

	return nil, nil
}

// Validate implements OpenStackBaremetalSetTemplateSpec validation
func (spec OpenStackBaremetalSetTemplateSpec) ValidateTemplate(oldCount int, oldSpec OpenStackBaremetalSetTemplateSpec) error {
	if oldCount > 0 &&
		(!equality.Semantic.DeepEqual(spec.BmhLabelSelector, oldSpec.BmhLabelSelector) ||
			!equality.Semantic.DeepEqual(spec.HardwareReqs, oldSpec.HardwareReqs)) {
		return fmt.Errorf("cannot change \"bmhLabelSelector\" nor \"hardwareReqs\" when previous count of \"baremetalHosts\" > 0")
	}
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *OpenStackBaremetalSet) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	openstackbaremetalsetlog.Info("validate update", "name", r.Name)

	var ok bool
	var oldInstance *OpenStackBaremetalSet

	if oldInstance, ok = old.(*OpenStackBaremetalSet); !ok {
		return nil, fmt.Errorf("runtime object is not an OpenStackBaremetalSet")
	}

	if err := r.Spec.ValidateTemplate(len(oldInstance.Spec.BaremetalHosts),
		oldInstance.Spec.OpenStackBaremetalSetTemplateSpec); err != nil {
		return nil, err
	}

	//
	// Force BmhLabelSelector and HardwareReqs to remain the same unless the *old* count of spec.BaremetalHosts was 0.
	// We do this to maintain consistency across the gathered list of BMHs during reconcile.
	//
	oldCount := len(oldInstance.Spec.BaremetalHosts)
	newCount := len(r.Spec.BaremetalHosts)

	if newCount != oldCount {
		//
		// Don't allow count changes if instance.Status.BaremetalHosts contains any
		// bmhRefs that are missing from Metal3 BMHs.  We need to force the user to
		// restore the old BMHs before allowing the OSBMS controller to perform any
		// operations for scaling up or down.
		//
		// TODO: Create a specific context here instead of passing TODO()?
		if err := VerifyAndSyncBaremetalStatusBmhRefs(context.TODO(), webhookClient, r); err != nil {
			return nil, err
		}

		//
		// Validate that there are enough available BMHs for a potential scale-up
		//
		if newCount > oldCount {
			// Every BMH available that matches our (optional) labels
			baremetalHostsList, err := GetBaremetalHosts(
				context.TODO(),
				webhookClient,
				r.Spec.BmhNamespace,
				r.Spec.BmhLabelSelector,
			)
			if err != nil {
				return nil, err
			}

			// All BMHs were are *already* using
			existingBaremetalHosts, err := GetBaremetalHosts(
				context.TODO(),
				webhookClient,
				r.Spec.BmhNamespace,
				labels.GetLabels(r, labels.GetGroupLabel(ServiceName), map[string]string{}),
			)
			if err != nil {
				return nil, err
			}

			if _, err := VerifyBaremetalSetScaleUp(openstackbaremetalsetlog, r, baremetalHostsList, existingBaremetalHosts); err != nil {
				return nil, err
			}
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *OpenStackBaremetalSet) ValidateDelete() (admission.Warnings, error) {
	openstackbaremetalsetlog.Info("validate delete", "name", r.Name)

	return nil, nil
}
