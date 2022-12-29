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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	vanilla_log "log"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"gopkg.in/yaml.v3"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"

	"github.com/pkg/errors"
	st4sdv1alpha1 "github.com/st4sd/st4sd-olm-deploy/api/v1alpha1"
)

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
	}

	logger.Info("Installing/Updating CRDs")

	err = resources.Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}

		helper := resource.NewHelper(info.Client, info.Mapping)

		if _, err := helper.Get(info.Namespace, info.Name); err != nil {
			if !apierrors.IsNotFound(err) {
				return errors.Wrap(err, "could not get information about the resource")
			}
			if _, err := helper.Create(info.Namespace, true, info.Object); err != nil {
				return errors.Wrap(err, "failed to create resource")
			} else {
				logger.Info("Created CRD " + info.Name)
			}
			return nil
		}

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

func HelmInstallUpgrade(
	helmChartPath string,
	configuration *st4sdv1alpha1.SimulationToolkitSpecSetup,
	namespace string,
	releaseName string,
	dryRun bool,
) error {
	logger := log.Log.WithName("helm")
	logger.Info("Preparing to install/update reployment",
		"helmChartPath", helmChartPath, "dryRun", dryRun, "configuration", configuration)
	values, err := ConfigurationToHelmValues(configuration)

	if err != nil {
		return err
	}
	settings := cli.New()
	actionConfig := new(action.Configuration)

	// VV: Use vanilla log here because helm log statements do not use "keyValue" pairs. This is incompatible with "log"
	if err := actionConfig.Init(settings.RESTClientGetter(), namespace, "", vanilla_log.Printf); err != nil {
		logger.Info("Could not initialize helm/action.Configuration")
		return err
	}

	chart, err := loader.Load(helmChartPath)
	if err != nil {
		return errors.Wrap(err, "Unable to load helm chart")
	}

	crd_path := filepath.Join(helmChartPath, "crd-workflow.yaml")

	// VV: Because of the way that helm treats CRDs (see comment in configurationToHelmValues())
	// we'll first create/Update the CRD here
	if !dryRun {
		err = UpdateCRD(crd_path, namespace, actionConfig.KubeClient)
		if err != nil {
			return err
		}
	}

	results := []*release.Release{}

	if !dryRun {
		// VV: If Helm release already exists in current namespace, update it. Otherwise, create it.
		results, err = actionConfig.Releases.List(func(rel *release.Release) bool {
			return rel.Name == releaseName && rel.Namespace == namespace
		})
	}

	releaseExists := (len(results) > 0)

	var release *release.Release
	logger.Info("Discovering exiting helm release in namespace",
		"releaseName", releaseName,
		"namespace", namespace,
		"releaseExists", releaseExists,
		"err", err,
		"numResults", len(results),
	)

	if releaseExists {
		client := action.NewUpgrade(actionConfig)
		client.Namespace = namespace
		client.DryRun = dryRun
		client.DryRun = dryRun
		client.Devel = true
		client.ReuseValues = true
		client.MaxHistory = 2
		release, err = client.Run(releaseName, chart, values)
	} else {
		client := action.NewInstall(actionConfig)
		client.Namespace = namespace
		client.ReleaseName = releaseName
		client.DryRun = dryRun
		client.Devel = true
		client.ClientOnly = dryRun
		client.IsUpgrade = true
		release, err = client.Run(chart, values)
	}

	if err != nil {
		return err
	}

	// VV: Now we need to manually do "oc import-image" i.e. create ImageStreamImport objects.
	// This step configures OpenShift to "import" the images and populate the ImageStreamTags of the
	// ImageStream objects we created via helm
	// logger.Info("The rendered manifest is", "manifest", release.Manifest)
	if !dryRun {
		err = TriggerDeploymentConfigs(release, actionConfig.KubeClient, namespace)
	}

	return err
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

		metadata := object["metadata"].(map[interface{}]interface{})
		imagestreamName := metadata["name"].(string)
		spec := object["spec"].(map[interface{}]interface{})
		tags := spec["tags"].([]interface{})

		for _, i := range tags {
			t := i.(map[interface{}]interface{})
			_from := t["from"].(map[interface{}]interface{})
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
	configuration *st4sdv1alpha1.SimulationToolkitSpecSetup) (map[string]interface{}, error) {
	fields := strings.Split(configuration.RouteDomain, ".")

	if len(fields) < 2 {
		err := fmt.Errorf("Expected RouteDomain to be in the form " +
			"of <CDBLabelGateway>.<domain>, but was " + configuration.RouteDomain)
		return nil, err
	}

	clusterRouteDomain := strings.Join(fields[1:], ".")
	datastoreIdentifier := configuration.DatastoreIdentifier
	if datastoreIdentifier == "" {
		datastoreIdentifier = fields[0]
	}

	values := map[string]interface{}{
		"pvcForWorkflowInstances":      configuration.PVCInstances,
		"pvcForDatastoreMongoDB":       configuration.PVCDatastore,
		"pvcForRuntimeServiceMetadata": configuration.PVCRuntimeService,

		"datastoreMongoDBSecretName": configuration.SecretDSMongoUserPass,
		"clusterRouteDomain":         clusterRouteDomain,
		"datastoreLabelGateway":      datastoreIdentifier,
		"installGithubSecretOAuth":   false,
	}

	return values, nil
}
