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

package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gopkg.in/yaml.v3"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	st4sdv1alpha1 "github.com/st4sd/st4sd-olm/api/v1alpha1"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	vanilla_log "log"
)

const (
	RELEASE_CLUSTER_SCOPED       = "st4sd-cluster-scoped"
	RELEASE_NAMESPACED_UNMANAGED = "st4sd-namespaced-unmanaged"
	RELEASE_NAMESPACED_MANAGED   = "st4sd-namespaced-managed"
)

const (
	PATTERN_FAILED_TO_PATCH = `cannot patch "(?P<Object>.+)" with kind (?P<Kind>[^:]+)`
)

func DiscoverClusterIngress() (string, error) {
	config, err := ctrl.GetConfig()

	if err != nil {
		return "", errors.Wrap(err, "unable to discover default cluster domain because I could not build a k8s config")
	}
	configV1Client, err := configv1.NewForConfig(config)

	if err != nil {
		return "", errors.Wrap(err, "unable to discover default cluster domain because I could not"+
			" build a configV1Client")
	}

	ingress, err := configV1Client.Ingresses().Get(context.TODO(), "cluster", v1.GetOptions{})

	if err != nil {
		return "", errors.Wrap(err, "unable to discover default cluster domain because I could not get the cluster Ingress")
	}

	return ingress.Spec.Domain, nil
}

func TriggerImportImage(
	kubeClient kube.Interface,
	namespace, imageUrl, imageName, tag string) error {
	contents := `kind: ImageStreamImport
apiVersion: image.openshift.io/v1
metadata:
  name: "%s"
  namespace: "%s"
spec:
  import: true
  images:
    - from:
        kind: DockerImage
        name: "%s"
      to:
        name: "%s"
      referencePolicy:
        type: ""
status: {}`
	logger := log.Log.WithName("triggerImportImage")
	contents = fmt.Sprintf(contents, imageName, namespace, imageUrl, tag)

	resources, err := kubeClient.Build(strings.NewReader(contents), true)

	if err != nil {
		return err
	}

	_, err = kubeClient.Create(resources)

	if err != nil {
		return errors.Wrapf(err, "could not build resource for importing image (namespace: %s, imageUrl: %s, imageName: %s, tag: %s, object: %+v)", namespace, imageUrl, imageName, tag, contents)
	}

	logger.Info("Created ImageStreamImport for " + imageName)

	return err
}

func UpdateCRD(path, namespace string, kubeClient kube.Interface) error {
	logger := log.Log.WithName("update-crd")
	logger.Info("Will update/install CRD outside of helm")

	yamlFile, err := os.Open(path)

	if err != nil {
		return errors.Wrapf(err, "could not open %s", path)
	}

	resources, err := kubeClient.Build(yamlFile, true)

	if err != nil {
		return errors.Wrapf(err, "could not parse %s", path)
	}

	for _, crd := range resources {
		info := crd.Object.GetObjectKind().GroupVersionKind()
		if info.Kind != "CustomResourceDefinition" {
			return fmt.Errorf("file %s contains non-CustomResourceDefinition object (%s)", path, info.Kind)
		}

		logger.Info("Installing/Updating CRD " + crd.ObjectName())
	}

	err = resources.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}

		helper := resource.NewHelper(info.Client, info.Mapping)

		if _, err := helper.Get(info.Namespace, info.Name); err != nil {
			if !apierrors.IsNotFound(err) {
				return errors.Wrap(err, "could not get information about the resource")
			}
			logger.Info("Will create CRD " + info.Name)
			if _, err := helper.Create(info.Namespace, true, info.Object); err != nil {
				return errors.Wrap(err, "failed to create resource")
			} else {
				logger.Info("Created CRD " + info.Name)
			}
			return nil
		}

		logger.Info("Will update CRD " + info.Name)

		if _, err = helper.Replace(info.Namespace, info.Name, true, info.Object); err != nil {
			return errors.Wrap(err, "failed to replace object")
		} else {
			logger.Info("Updated CRD " + info.Name)
		}

		return nil
	})

	if err != nil {
		return errors.Wrap(err, "could not create/update CRDs")
	}

	return nil
}

type RecordDeployedChart struct {
	Manifest string
}

func (r *RecordDeployedChart) Run(
	renderedManifests *bytes.Buffer,
) (modifiedManifests *bytes.Buffer, err error) {
	r.Manifest = renderedManifests.String()
	return renderedManifests, nil
}

func routeDelete(routeName, namespace, releaseName string, logger logr.Logger) error {
	logger.Info("OpenShift forbids patching the Route " + routeName +
		" in " + namespace + " - deleting the offending object")
	config, _ := ctrl.GetConfig()
	routeClient, err := routev1.NewForConfig(config)

	if err != nil {
		return errors.Wrapf(err, "unable to create a routeClient for deleting Route %s/%s", namespace, routeName)
	}

	err = routeClient.Routes(namespace).Delete(context.TODO(), routeName, v1.DeleteOptions{})
	return err
}

// Merges 2 maps of key: value pairs overriding @a values with those in @b.
// This is the algorithm that helm uses to merge multiple "values.yaml" files
// VV: From https://github.com/helm/helm/blob/main/pkg/cli/values/options.go
func MergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = MergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

func HelmDeployPart(
	namespace, releaseName, helmChartPath string, dryRun bool,
	configuration *st4sdv1alpha1.SimulationToolkitSpecSetup,
	chart *chart.Chart, actionConfig *action.Configuration,
	inNamespace []*release.Release,
) error {
	logger := log.Log.WithName("Deploy")
	values, err := ConfigurationToHelmValues(chart, configuration, releaseName)

	if err != nil {
		return err
	}

	// logger.Info("Generated values for "+releaseName, "values", values)

	var release *release.Release = nil

	for i, rel := range inNamespace {
		if rel.Name == releaseName && rel.Namespace == namespace {
			release = inNamespace[i]
			break
		}
	}

	logger.Info("Discovering exiting helm release in namespace",
		"releaseName", releaseName,
		"namespace", namespace,
		"releaseExists", release != nil,
		"err", err)

	if release != nil {
		logger.Info("Updating release", "releaseName", releaseName)
		client := action.NewUpgrade(actionConfig)

		// recorded := RecordDeployedChart{}
		// client.PostRenderer = &recorded
		client.Namespace = namespace
		client.DryRun = dryRun
		client.Devel = true
		if releaseName != RELEASE_CLUSTER_SCOPED {
			// VV: RELEASE_CLUSTER_SCOPED modifies ClusterRolebinding and SecurityContextConstraints. We
			// Do not want a user that has permissions to edit the values of the release to end up
			// giving themselves more privileges using this operator as a proxy to edit the cluster-scoped objects.
			// Therefore, THIS release will ALWAYS use whatever are the defaults in the helm chart.
			// All other releases, can use w/e the helm-release values are.

			values = MergeMaps(release.Config, values)
		}
		client.MaxHistory = 2

		max_retries := 10

		for max_retries >= 0 {
			release, err = client.Run(releaseName, chart, values)

			msg := ""
			if err != nil {
				msg = err.Error()

				r := regexp.MustCompile(PATTERN_FAILED_TO_PATCH)
				// VV: allPatchProblems has the format [match index][0: entire matched string, 1: Object, 2: Kind]
				allPatchProblems := r.FindAllStringSubmatch(msg, -1)

				retryThisHelmDeployment := true
				for _, s := range allPatchProblems {
					if len(s) == 3 {
						if s[2] == "Route" {
							// VV: OpenShift does not support patching the host of a route.
							// This can happen if a user deploys ST4SD and then decides that they'd like to use
							// a different route. We need to delete the offending Route object and retry.
							err = routeDelete(s[1], namespace, releaseName, logger)

							if err == nil {
								logger.Info("Successfully deleted Route " + namespace + "/" + s[1])
							} else {
								retryThisHelmDeployment = false
								break
							}
						}
					} else {
						retryThisHelmDeployment = false
						break
					}
				}
				if !retryThisHelmDeployment {
					break
				}
			} else {
				break
			}

			logger.Info("Retrying to deploy "+releaseName, "remainingRetries", max_retries, "errorMessage", msg)
			max_retries--
		}

		// VV: Uncomment to print the Manifest that Helm attempted to deploy (works even if chart is borked)
		// logger.Info("Rendered " + recorded.Manifest)
	} else {
		logger.Info("Installing release", "releaseName", releaseName)
		client := action.NewInstall(actionConfig)
		client.Namespace = namespace
		client.ReleaseName = releaseName
		client.DryRun = dryRun
		client.Devel = true
		client.ClientOnly = dryRun
		release, err = client.Run(chart, values)
	}

	if err != nil {
		return err
	}

	if !dryRun && releaseName == RELEASE_CLUSTER_SCOPED {
		crd_path := filepath.Join(helmChartPath, "crd-workflow.yaml")

		err = UpdateCRD(crd_path, namespace, actionConfig.KubeClient)

		if err != nil {
			logger.Info("Unable to update CRDs", "error", err)
			return err
		}
	}

	// VV: Now we need to manually do "oc import-image" i.e. create ImageStreamImport objects.
	// This step configures OpenShift to "import" the images and populate the ImageStreamTags of the
	// ImageStream objects we created via helm
	// logger.Info("The rendered manifest is", "manifest", release.Manifest)
	if !dryRun && releaseName == RELEASE_NAMESPACED_MANAGED {
		err = TriggerDeploymentConfigs(release, actionConfig.KubeClient, namespace)
	}

	return err
}

func renderValuesForChart(
	releaseName string, namespace string, release *release.Release, chart *chart.Chart,
	values map[string]interface{}, actionConfig *action.Configuration, logger logr.Logger) {
	options := chartutil.ReleaseOptions{
		Name:      releaseName,
		Namespace: namespace,
		Revision:  release.Version + 1,
		IsUpgrade: true,
	}
	newVals, _ := chartutil.ToRenderValues(chart, values, options, actionConfig.Capabilities)
	logger.Info("New values " + fmt.Sprintf("%+v", newVals))
}

func HelmDeploySimulationToolkit(
	helmChartPath string,
	configuration *st4sdv1alpha1.SimulationToolkitSpecSetup,
	namespace string,
	dryRun bool,
) error {
	logger := log.Log.WithName("helm")
	logger.Info("Preparing to install/update reployment",
		"helmChartPath", helmChartPath, "dryRun", dryRun, "configuration", configuration)

	chart, err := loader.Load(helmChartPath)
	if err != nil {
		return errors.Wrap(err, "Unable to load helm chart")
	}

	settings := cli.New()
	settings.SetNamespace(namespace)
	actionConfig := new(action.Configuration)

	// VV: Use vanilla log here because helm log statements do not use "keyValue" pairs.
	// This is incompatible with "log"
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, "", vanilla_log.Printf); err != nil {
		logger.Info("Could not initialize helm/action.Configuration")
		return err
	}

	inNamespace := []*release.Release{}

	if !dryRun {
		x, err := actionConfig.Releases.List(func(r *release.Release) bool {
			return r.Namespace == namespace
		})
		if err != nil {
			return err
		}
		inNamespace = x
	}

	releases := [3]string{RELEASE_CLUSTER_SCOPED, RELEASE_NAMESPACED_UNMANAGED, RELEASE_NAMESPACED_MANAGED}

	for _, releaseName := range releases {
		err = HelmDeployPart(
			namespace, releaseName, helmChartPath,
			dryRun, configuration, chart, actionConfig, inNamespace)

		if err != nil {
			return errors.Wrapf(err, "unable to deploy release %s", releaseName)
		}
	}

	return nil
}

func TriggerDeploymentConfigs(release *release.Release, kubeClient kube.Interface, namespace string) error {
	logger := log.Log.WithName("trigger-dc")
	logger.Info("triggering ImageStreamImport", "releaseName", release.Name)

	buf := bytes.NewBufferString(release.Manifest)
	dec := yaml.NewDecoder(buf)

	// VV: Find out the fully rendered ImageStream objects by looking at the manifest we ended up
	// deploying. The other option is to render the Chart with helm's engine but that involves
	// potentially contacting K8s. The "easy" way out is to unmarshal the rendered Manifest (YAML)
	// and then keep around just the ImageStream objects. Finally, iterate their spec.tags and
	// create the associated ImageStreamImport via triggerImportImage()
	for {
		// VV: Read the YAML documents one by one and decide whether they should be filtered out or not
		object := make(map[string]interface{})
		if err := dec.Decode(&object); err != nil {
			if err != io.EOF {
				errors.Wrap(err, "Unable to parse manifests due to ")
				return err
			}
			break
		}

		kind := object["kind"].(string)

		if kind != "ImageStream" {
			continue
		}

		metadata := object["metadata"].(map[string]interface{})
		imagestreamName := metadata["name"].(string)
		spec := object["spec"].(map[string]interface{})
		tags := spec["tags"].([]interface{})

		for _, i := range tags {
			t := i.(map[string]interface{})
			_from := t["from"].(map[string]interface{})
			imageUrl := _from["name"].(string)
			tag := t["name"].(string)

			// VV: Throw away leading ':', we could parse the imageUrl but in the future we may
			// decide to use hashes so let's just avoid that for now.
			if tag[0] != ':' {
				return fmt.Errorf("invalid tag %s for ImageStream %s", tag, imagestreamName)
			}
			tag = tag[1:]

			err := TriggerImportImage(kubeClient, namespace, imageUrl, imagestreamName, tag)

			if err != nil {
				return err
			}
		}
	}

	logger.Info("Finished triggering ImageStreamImport")

	return nil
}

func ConfigurationToHelmValues(
	chart *chart.Chart,
	configuration *st4sdv1alpha1.SimulationToolkitSpecSetup,
	releaseName string,
) (map[string]interface{}, error) {
	fields := strings.Split(configuration.RouteDomain, ".")

	if len(fields) < 2 {
		err := fmt.Errorf("Expected RouteDomain to be in the form " +
			"of <datastoreIdentifier>.<domain>, but was " + configuration.RouteDomain)
		return nil, err
	}

	clusterRouteDomain := strings.Join(fields[1:], ".")
	datastoreIdentifier := configuration.DatastoreIdentifier
	if datastoreIdentifier == "" {
		datastoreIdentifier = fields[0]
	}
	routePrefix := fields[0]

	values := map[string]interface{}{
		"pvcForWorkflowInstances":      configuration.PVCInstances,
		"pvcForDatastoreMongoDB":       configuration.PVCDatastore,
		"pvcForRuntimeServiceMetadata": configuration.PVCRuntimeService,
		"clusterRouteDomain":           clusterRouteDomain,
		"datastoreLabelGateway":        datastoreIdentifier,

		"installImagePullSecretWorkflowStack":         false,
		"installImagePullSecretContribApplications":   false,
		"installImagePullSecretCommunityApplications": false,
		"useImagePullSecretWorkflowStack":             false,
		"useImagePullSecretContribApplications":       false,
		"useImagePullSecretCommunityApplications":     false,
	}

	if configuration.SecretDSMongoUserPass != "" {
		values["datastoreMongoDBSecretName"] = configuration.SecretDSMongoUserPass
	}

	switchOn := []string{}
	switchOff := []string{}

	switch releaseName {
	case RELEASE_CLUSTER_SCOPED:
		switchOn = append(switchOn, "installRBACClusterScoped")
		switchOff = append(switchOff,
			"installDatastoreSecretMongoDB", "installRuntimeServiceConfigMap",
			"installRegistryBackendConfigMap", "installRegistryUINginxConfigMap",
			"installWorkflowOperator", "installDatastore", "installRuntimeService",
			"installRegistryBackend", "installRegistryUI", "installAuthentication",
			"installRBACNamespaced", "installDeployer",
		)
	case RELEASE_NAMESPACED_UNMANAGED:
		switchOn = append(switchOn,
			"installDatastoreSecretMongoDB", "installRuntimeServiceConfigMap",
			"installRegistryBackendConfigMap", "installRegistryUINginxConfigMap",
		)
		switchOff = append(switchOff,
			"installRBACClusterScoped",
			"installWorkflowOperator", "installDatastore", "installRuntimeService",
			"installRegistryBackend", "installRegistryUI", "installAuthentication",
			"installRBACNamespaced", "installDeployer",
		)
		// VV: st4sd-deployment pushes 2 sets of images for each release:
		// :platform-release-latest and :bundle-${HELM_CHART_VERSION}
		// RELEASE_NAMESPACED_UNMANAGED uses the imagesVariant value to populate the
		// st4sd-runtime-service ConfigMap
		values["imagesVariant"] = fmt.Sprintf(":bundle-%s", chart.AppVersion())
		values["routePrefix"] = routePrefix
	case RELEASE_NAMESPACED_MANAGED:
		switchOn = append(switchOn,
			"installWorkflowOperator", "installDatastore", "installRuntimeService",
			"installRegistryBackend", "installRegistryUI", "installAuthentication",
			"installRBACNamespaced", "installDeployer",
		)
		switchOff = append(switchOff,
			"installRBACClusterScoped",
			"installDatastoreSecretMongoDB", "installRuntimeServiceConfigMap",
			"installRegistryBackendConfigMap", "installRegistryUINginxConfigMap", "installDeployer",
		)
		// RELEASE_NAMESPACED_UNMANAGED uses the imagesVariant value to populate the
		// DeploymentConfig objects
		values["imagesVariant"] = fmt.Sprintf(":bundle-%s", chart.AppVersion())
		values["routePrefix"] = routePrefix
	default:
		return values, fmt.Errorf("unknown release %s", releaseName)
	}

	for _, k := range switchOn {
		values[k] = true
	}

	for _, k := range switchOff {
		values[k] = false
	}

	return values, nil
}
