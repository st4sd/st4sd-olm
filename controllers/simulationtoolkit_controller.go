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
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/chart/loader"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/pkg/errors"
	deployv1alpha1 "github.com/st4sd/st4sd-olm/api/v1alpha1"
	"github.com/st4sd/st4sd-olm/deploy"
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

//+kubebuilder:rbac:groups=deploy.st4sd.ibm.com,resources=simulationtoolkits,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=deploy.st4sd.ibm.com,resources=simulationtoolkits/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=deploy.st4sd.ibm.com,resources=simulationtoolkits/finalizers,verbs=update

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
	var err error = nil

	phase := obj.Status.Phase
	versionID := obj.Status.VersionID

	if updateEntireObject {
		err = r.Update(ctx, obj)
	}

	if err != nil {
		return err
	}

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

	obj.Status.Phase = phase
	obj.Status.VersionID = versionID

	err = r.Status().Update(ctx, obj)

	if err != nil {
		return err
	}

	// VV: The object is now different, get the most up-to-date version
	err = r.Get(ctx, types.NamespacedName{Namespace: obj.ObjectMeta.Namespace, Name: obj.ObjectMeta.Name}, obj)

	return err
}

func (r *SimulationToolkitReconciler) ExpectedVersion() string {
	return strings.Join([]string{deployv1alpha1.OPERATOR_VERSION, r.HelmChartVersion, r.ToolkitVersion}, "/")
}

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

	lastCondition := deployv1alpha1.SimulationToolkitStatusCondition{}
	allConditions := map[string]deployv1alpha1.SimulationToolkitStatusCondition{}

	addBackToQueue := false

	for i := range obj.Status.Conditions {
		c := obj.Status.Conditions[i]
		allConditions[c.Status] = c

		if lastCondition.Status == deployv1alpha1.STATUS_UNKNOWN ||
			(c.Status != deployv1alpha1.STATUS_UNKNOWN &&
				c.LastTransitionTime.UnixMilli() > lastCondition.LastTransitionTime.UnixMilli()) {
			lastCondition = c
		}
	}

	hashCurrent := obj.Spec.Setup.Hash()
	if obj.GetAnnotations() == nil {
		annotations := map[string]string{annotationLastConfigurationKey: hashCurrent}
		obj.SetAnnotations(annotations)
	}

	if obj.Spec.Paused {
		paused := allConditions[deployv1alpha1.STATUS_PAUSED]

		paused.Status = deployv1alpha1.STATUS_PAUSED
		paused.LastUpdateTime = v1.NewTime(time.Now())
		paused.LastTransitionTime = paused.LastUpdateTime
		if paused.Message == "" {
			paused.Message = "Pausing deployment because setup.paused=true"
		}

		if paused.VersionID == "" {
			paused.VersionID = r.ExpectedVersion()
		}

		obj.Status.Phase = deployv1alpha1.STATUS_PAUSED
		allConditions[deployv1alpha1.STATUS_PAUSED] = paused
	} else {
		// VV: Here we know that we hope to install or update ST4SD

		transitionToUpdating := func(message, reason string) {
			updating := allConditions[deployv1alpha1.STATUS_UPDATING]

			updating.Status = deployv1alpha1.STATUS_UPDATING
			updating.LastUpdateTime = v1.NewTime(time.Now())
			updating.LastTransitionTime = updating.LastUpdateTime
			updating.Message = message
			updating.Reason = reason

			updating.VersionID = r.ExpectedVersion()
			obj.Status.Phase = deployv1alpha1.STATUS_UPDATING
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
		configurationChanged := hashCurrent != hashLast
		configurationOld := now.Unix()-lastCondition.LastTransitionTime.Unix() > STALE_THRESHOLD_SECONDS
		deploymentStale := r.ExpectedVersion() != lastCondition.VersionID
		obj.Status.VersionID = r.ExpectedVersion()

		logger.Info(
			"Handling new object", "name", obj.Name, "namespace", obj.Namespace, "lastCondition",
			lastCondition, "configurationChanged", configurationChanged,
			"configurationOld", configurationOld, "deploymentStale", deploymentStale,
			"hashLast", hashLast, "hashCurrent", hashCurrent,
		)
		soon := "WillStartUpdatingSoon"
		switch lastCondition.Status {
		case deployv1alpha1.STATUS_UNKNOWN:
			transitionToUpdating("Starting deployment because setup.paused=false", soon)
		case deployv1alpha1.STATUS_SUCCESSFUL:
			if configurationChanged {
				transitionToUpdating("Updating Successful deployment to apply new Setup configuration", soon)
			} else if deploymentStale {
				transitionToUpdating("Updating Successful deployment to apply new Helm chart", soon)
			}
		case deployv1alpha1.STATUS_FAILED:
			if configurationChanged {
				transitionToUpdating("Retrying Failed deployment to apply new Setup configuration", soon)
			} else if configurationOld {
				why := fmt.Sprintf("Retrying Failed deployment after waiting for %d seconds", secondsDt)
				transitionToUpdating(why, soon)
			} else if deploymentStale {
				transitionToUpdating("Retrying Failed deployment to apply new Helm chart", soon)
			} else {
				// VV: We don't want to update the deployment right now, we may want to update it later
				addBackToQueue = true
			}
		case deployv1alpha1.STATUS_UPDATING:
			obj.ObjectMeta.Annotations[annotationLastConfigurationKey] = hashCurrent

			transitionToUpdating("Deploying ST4SD now", "Updating")
			err = r.UpdateStatus(ctx, obj, allConditions, true)
			if err != nil {
				logger.Error(err, "Could not update status")
				return r.Requeue(err)
			}

			// VV: Right before we deploy this, check if `routeDomain` is unset. If so, try to discover it.
			// If that fails, then we simply cannot deploy ST4SD here unless the user tells us which domain to use
			var err error = nil

			requiresClusterDomain := false
			ingress := ""

			if (obj.Spec.Setup.RouteDomain == "" && obj.Spec.Setup.DatastoreIdentifier != "") ||
				strings.Contains(obj.Spec.Setup.RouteDomain, deployv1alpha1.INTERPOLATE_CLUSTER_INGRESS) {
				requiresClusterDomain = true
			} else if obj.Spec.Setup.DatastoreIdentifier == "" && obj.Spec.Setup.RouteDomain == "" {
				err = fmt.Errorf("unable to auto-generate routeDomain because both " +
					"datastoreIdentifier and routeDomain are unset")
			}

			if err == nil && requiresClusterDomain {
				ingress, err = deploy.DiscoverClusterIngress()
				if err != nil {
					err = errors.Wrap(err, "unable to auto-generate routeDomain")
				}
			}

			if err == nil && requiresClusterDomain {
				if obj.Spec.Setup.RouteDomain == "" {
					// VV: If the RouteDomain is empty then use ${datastoreIdentifier}-${namespace}.${ingress}
					routeDomain := fmt.Sprintf("%s-%s.%s", obj.Spec.Setup.DatastoreIdentifier,
						req.NamespacedName.Namespace, ingress)
					obj.Spec.Setup.RouteDomain = routeDomain
				} else {
					// VV: if the RouteDomain is set, then replace ${CLUSTER_INGRESS} with the cluster ingress
					// The user decided how they wish their routeDomain to look like, there's no need for us
					// to inject the namespace in the domain.
					obj.Spec.Setup.RouteDomain = strings.ReplaceAll(
						obj.Spec.Setup.RouteDomain, deployv1alpha1.INTERPOLATE_CLUSTER_INGRESS, ingress)
				}
			}

			if err == nil && obj.Spec.Setup.DatastoreIdentifier == "" {
				fields := strings.Split(obj.Spec.Setup.RouteDomain, ".")

				if len(fields) < 2 {
					err = fmt.Errorf("expected RouteDomain to be in the form "+
						"of <datastoreIdentifier>.<domain>, but was %s", obj.Spec.Setup.RouteDomain)
				} else {
					obj.Spec.Setup.DatastoreIdentifier = fields[0]
				}
			}

			if err == nil {
				err = deploy.HelmDeploySimulationToolkit(r.HelmChartPath, &obj.Spec.Setup,
					req.NamespacedName.Namespace, false)
			}

			if err == nil {
				status := deployv1alpha1.STATUS_SUCCESSFUL
				successful := allConditions[status]

				successful.Status = status
				successful.LastUpdateTime = v1.NewTime(time.Now())
				successful.LastTransitionTime = successful.LastUpdateTime
				successful.Message = "ST4SD deployed, enjoy!"
				successful.Reason = "Success"

				successful.VersionID = r.ExpectedVersion()
				obj.Status.Phase = deployv1alpha1.STATUS_SUCCESSFUL
				allConditions[status] = successful
			} else {
				status := deployv1alpha1.STATUS_FAILED
				failed := allConditions[status]

				failed.Status = status
				failed.LastUpdateTime = v1.NewTime(time.Now())
				failed.LastTransitionTime = failed.LastUpdateTime
				failed.Message = fmt.Sprint("Failed to deploy ST4SD.", err)
				failed.Reason = "HelmDeploymentFailed"

				obj.Status.Phase = deployv1alpha1.STATUS_FAILED

				failed.VersionID = r.ExpectedVersion()

				allConditions[status] = failed

				logger.Info("Failed to deploy ST4SD", "error", err)
				addBackToQueue = true
			}
		}
	}

	err = r.UpdateStatus(ctx, obj, allConditions, true)
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
