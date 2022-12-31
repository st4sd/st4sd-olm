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
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/chart/loader"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	deployv1alpha1 "github.com/st4sd/st4sd-olm-deploy/api/v1alpha1"
	"github.com/st4sd/st4sd-olm-deploy/deploy"
)

const (
	annotationLastConfigurationKey = "st4sd.ibm.com/last-configuration"
	STALE_THRESHOLD_SECONDS        = 5 * 60
)

// SimulationToolkitReconciler reconciles a SimulationToolkit object
type SimulationToolkitReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Version of the Simulation Toolkit (from the helm-chart bundled alongside the operator)
	ToolkitVersion string
	// Version of the helm-chart that deploys the toolkit (from the helm-chart
	// bundled alongside the operator)
	HelmChartVersion string
	HelmChartPath    string
}

//+kubebuilder:rbac:groups=deploy.st4sd.ibm.com,resources=simulation-toolkits,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deploy.st4sd.ibm.com,resources=simulation-toolkits/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deploy.st4sd.ibm.com,resources=simulation-toolkits/finalizers,verbs=update

// SetupWithManager sets up the controller with the Manager.
func (r *SimulationToolkitReconciler) SetupWithManager(mgr ctrl.Manager) error {
	chart, err := loader.Load(r.HelmChartPath)

	if err != nil {
		return err
	}

	r.HelmChartVersion = chart.Metadata.Version
	r.ToolkitVersion = chart.AppVersion()

	return ctrl.NewControllerManagedBy(mgr).
		For(&deployv1alpha1.SimulationToolkit{}).
		Complete(r)
}

func (r *SimulationToolkitReconciler) UpdateStatus(
	ctx context.Context, obj *deployv1alpha1.SimulationToolkit,
	allConditions map[string]deployv1alpha1.SimulationToolkitStatusCondition,
	updateEntireObject bool,
) error {
	obj.Status.Conditions = make([]deployv1alpha1.SimulationToolkitStatusCondition, len(allConditions))
	idx := 0

	for _, key := range []string{
		deployv1alpha1.STATUS_PAUSED, deployv1alpha1.STATUS_FAILED,
		deployv1alpha1.STATUS_SUCCESSFUL, deployv1alpha1.STATUS_UPDATING} {
		if c, ok := allConditions[key]; ok {
			obj.Status.Conditions[idx] = c
			idx++
		}
	}
	var err error = nil

	if updateEntireObject {
		err = r.Update(ctx, obj)
	} else {
		err = r.Status().Update(ctx, obj)
	}

	return err
}

//+kubebuilder:rbac:groups=core.st4sd.ibm.com,resources=st4sdruntimes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.st4sd.ibm.com,resources=st4sdruntimes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.st4sd.ibm.com,resources=st4sdruntimes/finalizers,verbs=update

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *SimulationToolkitReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	obj := &deployv1alpha1.SimulationToolkit{}
	err := r.Get(ctx, req.NamespacedName, obj)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("resource not found - ignoring since object must have been deleted.")

			return r.DoNotRequeue(nil)
		} else {
			return r.Requeue(err)
		}
	}

	// VV: Do not bother with objects that are marked for Deletion - we *could* consider that this means
	// a user wishes to un-deploy but let the system admin deal with this scenario.
	// They just need to delete the helm releases.
	if obj.GetDeletionTimestamp() != nil {
		r.DoNotRequeue(nil)
	}

	var lastCondition *deployv1alpha1.SimulationToolkitStatusCondition = nil
	allConditions := map[string]deployv1alpha1.SimulationToolkitStatusCondition{}

	addBackToQueue := false
	updateEntireObject := false

	for i := range obj.Status.Conditions {
		c := obj.Status.Conditions[i]
		allConditions[c.Status] = obj.Status.Conditions[i]

		if lastCondition == nil ||
			c.LastTransitionTime.UnixMilli() > lastCondition.LastTransitionTime.UnixMilli() {
			lastCondition = &obj.Status.Conditions[i]
		}
	}

	if lastCondition == nil {
		lastCondition = &deployv1alpha1.SimulationToolkitStatusCondition{
			Status: deployv1alpha1.STATUS_UNKNOWN,
		}
	}

	if obj.ObjectMeta.Annotations == nil {
		obj.ObjectMeta.Annotations = make(map[string]string)
	}

	if obj.Spec.Paused {
		paused := allConditions[deployv1alpha1.STATUS_PAUSED]

		paused.Status = deployv1alpha1.STATUS_PAUSED
		paused.LastUpdateTime = v1.NewTime(time.Now())
		paused.LastTransitionTime = paused.LastUpdateTime
		if paused.Message == "" {
			paused.Message = "Pausing deployment because setup.paused=true"
		}

		allConditions[deployv1alpha1.STATUS_PAUSED] = paused
	} else {
		// VV: Here we know that we hope to install or update ST4SD

		transitionToUpdating := func(message string) {
			updating := allConditions[deployv1alpha1.STATUS_UPDATING]

			updating.Status = deployv1alpha1.STATUS_UPDATING
			updating.LastUpdateTime = v1.NewTime(time.Now())
			updating.LastTransitionTime = updating.LastUpdateTime
			updating.Message = message
			updating.Reason = "WillStartUpdatingSoon"

			updating.HelmChartVersion = r.HelmChartVersion
			updating.ToolkitVersion = r.ToolkitVersion

			allConditions[deployv1alpha1.STATUS_UPDATING] = updating
		}

		/* VV: State machine details:
		Unknown => Updating
		Updating => if update successful Successful, else Failed
		Failed => if (transitioned to failed less than 5 minutes ago)
			OR (configuration has changed since last time we deployed/updated):

		If configuration has changed since last time we deployed/updated:
		  Successful => Updating
		Else:
			stay in the same
		*/
		now := v1.NewTime(time.Now())
		secondsDt := now.Unix() - lastCondition.LastTransitionTime.Unix()
		hashLast := obj.ObjectMeta.Annotations[annotationLastConfigurationKey]
		hashCurrent := obj.Spec.Setup.HashBase64()
		configurationChanged := hashCurrent != hashLast
		configurationOld := now.Unix()-lastCondition.LastTransitionTime.Unix() > STALE_THRESHOLD_SECONDS
		deploymentStale := (r.HelmChartVersion != lastCondition.HelmChartVersion) ||
			(r.ToolkitVersion != lastCondition.ToolkitVersion)

		switch lastCondition.Status {
		case deployv1alpha1.STATUS_UNKNOWN:
			transitionToUpdating("Starting deployment because setup.paused=false")
		case deployv1alpha1.STATUS_SUCCESSFUL:
			if configurationChanged {
				transitionToUpdating("Updating Successful deployment to apply new Setup configuration")
			} else if deploymentStale {
				transitionToUpdating("Updating Successful deployment to apply new Helm chart")
			}
		case deployv1alpha1.STATUS_FAILED:
			if configurationChanged {
				transitionToUpdating("Retrying Failed deployment to apply new Setup configuration")
			} else if configurationOld {
				why := fmt.Sprintf("Retrying Failed deployment after waiting for %d seconds", secondsDt)
				transitionToUpdating(why)
			} else if deploymentStale {
				transitionToUpdating("Retrying Failed deployment to apply new Helm chart")
			}
		case deployv1alpha1.STATUS_UPDATING:
			obj.ObjectMeta.Annotations[annotationLastConfigurationKey] = hashCurrent
			updateEntireObject = true
			err := deploy.HelmDeploySimulationToolkit(r.HelmChartPath, &obj.Spec.Setup,
				req.NamespacedName.Namespace, false)
			if err == nil {
				status := deployv1alpha1.STATUS_SUCCESSFUL
				successful := allConditions[status]

				successful.Status = status
				successful.LastUpdateTime = v1.NewTime(time.Now())
				successful.LastTransitionTime = successful.LastUpdateTime
				successful.Message = "ST4SD deployed, enjoy!"
				successful.Reason = "Success"

				successful.HelmChartVersion = r.HelmChartVersion
				successful.ToolkitVersion = r.ToolkitVersion

				allConditions[status] = successful
			} else {
				status := deployv1alpha1.STATUS_FAILED
				failed := allConditions[status]

				failed.Status = status
				failed.LastUpdateTime = v1.NewTime(time.Now())
				failed.LastTransitionTime = failed.LastUpdateTime
				failed.Message = fmt.Sprint("Failed to deploy ST4SD.", err)
				failed.Reason = "HelmDeploymentFailed"

				failed.HelmChartVersion = r.HelmChartVersion
				failed.ToolkitVersion = r.ToolkitVersion

				allConditions[status] = failed
				addBackToQueue = true
			}
		}
	}

	err = r.UpdateStatus(ctx, obj, allConditions, updateEntireObject)
	if err != nil {
		logger.Error(err, "Could not update status")
		return r.Requeue(err)
	}

	if addBackToQueue {
		return r.Requeue(nil)
	} else {
		return r.DoNotRequeue(nil)
	}
}

func (r *SimulationToolkitReconciler) DoNotRequeue(with_error error) (reconcile.Result, error) {
	return ctrl.Result{}, with_error
}

func (r *SimulationToolkitReconciler) Requeue(with_error error) (reconcile.Result, error) {
	return ctrl.Result{Requeue: true}, with_error
}
