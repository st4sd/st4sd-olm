/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

Authors:
  Vassilis Vassiliadis
*/

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	deployv1alpha1 "github.com/st4sd/st4sd-olm-deploy/api/v1alpha1"
)

var (
	annotationLastConfigurationKey = "st4sd.res.ibm.com/last-configuration"
)

// SimulationToolkitReconciler reconciles a SimulationToolkit object
type SimulationToolkitReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=deploy.st4sd.res.ibm.com,resources=simulation-toolkits,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deploy.st4sd.res.ibm.com,resources=simulation-toolkits/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deploy.st4sd.res.ibm.com,resources=simulation-toolkits/finalizers,verbs=update

// SetupWithManager sets up the controller with the Manager.
func (r *SimulationToolkitReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&deployv1alpha1.SimulationToolkit{}).
		Complete(r)
}

//+kubebuilder:rbac:groups=core.st4sd.ibm.com,resources=st4sdruntimes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.st4sd.ibm.com,resources=st4sdruntimes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.st4sd.ibm.com,resources=st4sdruntimes/finalizers,verbs=update

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *SimulationToolkitReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	obj_cr := &deployv1alpha1.SimulationToolkit{}
	err := r.Get(ctx, req.NamespacedName, obj_cr)

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("ST4SDRuntime resource not found. Ignoring since object must have been deleted.")

			return r.DoNotRequeue(nil)
		} else {
			return r.Requeue(err)
		}
	}

	// VV: Do not bother with objects that are marked for Deletion - we *could* consider that this means
	// a user wishes to un-deploy but let the system admin deal with this scenario.
	// They just need to delete the helm releases.
	if obj_cr.GetDeletionTimestamp() != nil {
		r.DoNotRequeue(nil)
	}

	var lastCondition *deployv1alpha1.SimulationToolkitStatusCondition = nil
	allConditions := map[string]*deployv1alpha1.SimulationToolkitStatusCondition{}

	for i := range obj_cr.Status.Conditions {
		c := obj_cr.Status.Conditions[i]
		allConditions[c.Status] = &obj_cr.Status.Conditions[i]

		if lastCondition == nil ||
			c.LastTransitionTime.UnixMilli() > lastCondition.LastTransitionTime.UnixMilli() {
			lastCondition = &obj_cr.Status.Conditions[i]
		}
	}

	if lastCondition == nil {
		lastCondition = &deployv1alpha1.SimulationToolkitStatusCondition{
			Status: deployv1alpha1.STATUS_UNKNOWN,
		}
	}

	// VV: If state is unset, object is either Paused Or about to start Updating
	// if the deployment is Paused but its state is something else then mark the State as Paused
	// and never go back to it
	if lastCondition.Status == deployv1alpha1.STATUS_UNKNOWN {
		if obj_cr.Spec.Paused {
			obj_cr.Status.State = st4sd.STATE_PAUSED
		} else {
			obj_cr.Status.State = st4sd.STATE_UPDATING
		}

		err = r.Status().Update(ctx, obj_cr)
		if err != nil {
			logger.Info("Could not update status", "status.state", obj_cr.Status.State, "err", err)
			return r.Requeue(err)
		}
	} else if obj_cr.Status.State != st4sd.STATE_PAUSED && obj_cr.Spec.Paused {
		obj_cr.Status.State = st4sd.STATE_PAUSED

		err = r.Status().Update(ctx, obj_cr)
		if err != nil {
			logger.Info("Could not update status", "status.state", obj_cr.Status.State, "err", err)
			return r.Requeue(err)
		}
	}

	if obj_cr.ObjectMeta.Annotations == nil {
		obj_cr.ObjectMeta.Annotations = make(map[string]string)
	}
	lastConfiguration := obj_cr.ObjectMeta.Annotations[annotationLastConfigurationKey]

	// VV: Here we look at the annotation to make sure that the Configuration
	// has not changed since the last time we updated the deployment.
	// This will trigger an update if the CRD changes after the runtime
	// has already been deployed once
	currentHash := obj_cr.Spec.Configuration.HashBase64()
	if lastConfiguration != currentHash {
		if obj_cr.Status.State != st4sd.STATE_UPDATING {
			logger.Info("Last known configuration mismatch - setting state to "+st4sd.STATE_UPDATING,
				"recordedState", obj_cr.Status.State, "lastConfiguration", lastConfiguration,
				"currentHash", currentHash)
			obj_cr.ObjectMeta.Annotations[annotationLastConfigurationKey] = currentHash

			err = r.Update(ctx, obj_cr)
			if err != nil {
				logger.Info("Could not update annotations", "err", err)
				return r.Requeue(err)
			}

			obj_cr.Status.State = st4sd.STATE_UPDATING
			err = r.Status().Update(ctx, obj_cr)
			if err != nil {
				logger.Info("Could not update status", "err", err)
				return r.Requeue(err)
			}
		}
	}

	// VV: State machine
	// Paused -> *Paused (do not requeue)
	// Failed -> *Failed (do not requeue)
	// Updating -> Successful      - if deployment is Successful
	// Updating -> Failed          - if deployment Failed
	// Updating -> Testing (TBD)   - if spec.TestAfterDeployment is True
	// Testing -> Successful (TBD) - if testing is Successful
	// Testing -> Failed     (TBD) - if testing Failed

	switch obj_cr.Status.State {

	case st4sd.STATE_SUCCESSFUL:
	case st4sd.STATE_FAILED:
	case st4sd.STATE_PAUSED:
		logger.Info(obj_cr.Name + " is already " + obj_cr.Status.State + ", nothing more to do")
		return r.DoNotRequeue(nil)

	case st4sd.STATE_UPDATING:
		configuration := obj_cr.Spec.Configuration
		namespacedObjectsOnly := (r.DeployObjects == "namespaced-only")
		logger.Info("Handling ST4SDRuntimeConfiguration", "configuration", configuration,
			"namespacedObjectsOnly", namespacedObjectsOnly)

		err := st4sd.HelmInstallUpgrade("/chart", &configuration, namespacedObjectsOnly,
			req.NamespacedName.Namespace, "st4sd-runtime-managed", false)
		if err != nil {
			logger.Info("Unable to install/upgrade helm", "err", err)
			obj_cr.Status.State = st4sd.STATE_FAILED
		} else {
			logger.Info("Success")
			if obj_cr.Spec.TestAfterDeployment {
				obj_cr.Status.State = st4sd.STATE_TESTING
			} else {
				obj_cr.Status.State = st4sd.STATE_SUCCESSFUL
			}
		}

		err = r.Status().Update(ctx, obj_cr)
		return r.Requeue(nil)
	}

	return r.DoNotRequeue(nil)
}

func (r *SimulationToolkitReconciler) DoNotRequeue(with_error error) (reconcile.Result, error) {
	return ctrl.Result{}, with_error
}

func (r *SimulationToolkitReconciler) Requeue(with_error error) (reconcile.Result, error) {
	return ctrl.Result{Requeue: true}, with_error
}
